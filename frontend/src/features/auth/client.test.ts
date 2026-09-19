// @vitest-environment jsdom
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { session, signIn, signOut, subscribeAuth } from "./client";
const listeners: FakeChannel[] = [];
class FakeChannel {
  onmessage: ((event: { data: unknown }) => void) | null = null;
  close = vi.fn();
  postMessage = vi.fn();
  constructor() { listeners.push(this); }
}
const fetchMock = vi.fn();
beforeEach(() => {
  listeners.length = 0; vi.stubGlobal("BroadcastChannel", FakeChannel); vi.stubGlobal("fetch", fetchMock); fetchMock.mockReset();
  Object.defineProperty(navigator, "locks", { configurable: true, value: { request: vi.fn((_name, callback) => callback()) } });
});
afterEach(() => vi.unstubAllGlobals());
it("deduplicates concurrent checks", async () => {
  fetchMock.mockResolvedValueOnce(Response.json({ user: { email: "x@example.com" } }));
  const a = session(), b = session(); expect(a).toBe(b); await a;
  expect(fetchMock).toHaveBeenCalledTimes(1); expect(navigator.locks.request).toHaveBeenCalledWith("uptime-auth", expect.any(Function));
});
it("does not retry uncertain failures automatically", async () => {
  fetchMock.mockRejectedValueOnce(new Error("network")); await expect(session()).rejects.toThrow("Connection interrupted"); expect(fetchMock).toHaveBeenCalledTimes(1);
});
it("uses same-origin cookies and notifies on login/logout without tokens", async () => {
  fetchMock.mockImplementation(async () => Response.json({ user: { email: "x@example.com" } }));
  await signIn("login", "x@example.com", "long password"); await signOut();
  expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: "same-origin", method: "POST" });
  expect(listeners).toHaveLength(2); for (const l of listeners) expect(l.postMessage).toHaveBeenCalledWith("changed");
});
it("subscribes only to known events and closes the channel", () => {
  const listener = vi.fn(); const close = subscribeAuth(listener); listeners[0].onmessage?.({ data: "other" }); expect(listener).not.toHaveBeenCalled(); listeners[0].onmessage?.({ data: "changed" }); expect(listener).toHaveBeenCalledOnce(); close(); expect(listeners[0].close).toHaveBeenCalledOnce();
});
it("fails safely without Web Locks", async () => {
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined }); await expect(session()).rejects.toThrow("update your browser"); expect(fetchMock).not.toHaveBeenCalled();
});
