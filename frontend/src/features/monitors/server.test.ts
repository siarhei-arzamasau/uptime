import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { handleMonitors } from "./server";

const monitor = { id: "monitor-id", url: "https://example.com", interval_seconds: 7, created_at: "2026-09-22" };
const fetchMock = vi.fn<typeof fetch>();
function req(body?: unknown, cookie = "uptime_access=valid", headers = {}) {
  return new NextRequest("http://localhost:3000/api/auth/monitors", { method: "POST", headers: { Origin: "http://localhost:3000", "X-CSRF-Protection": "1", "Content-Type": "application/json", Cookie: cookie, ...headers }, body: body === undefined ? undefined : JSON.stringify(body) });
}
const input = { url: monitor.url, interval_seconds: monitor.interval_seconds };
beforeEach(() => { vi.stubGlobal("fetch", fetchMock); fetchMock.mockReset(); vi.stubEnv("APP_ORIGIN", "http://localhost:3000"); });
afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs(); });

it("creates and lists monitors while exposing only public fields", async () => {
  fetchMock.mockResolvedValueOnce(Response.json({ ...monitor, user_id: "private", access_token: "secret" }));
  const created = await handleMonitors(req(input), "create");
  expect(created.status).toBe(201); expect(await created.json()).toEqual({ monitor });
  expect(fetchMock).toHaveBeenLastCalledWith("http://127.0.0.1:8080/api/v1/monitors", expect.objectContaining({ method: "POST", body: JSON.stringify(input), cache: "no-store" }));
  expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get("Authorization")).toBe("Bearer valid");
  fetchMock.mockResolvedValueOnce(Response.json({ monitors: [{ ...monitor, user_id: "private" }] }));
  const listed = await handleMonitors(req(), "list");
  expect(await listed.json()).toEqual({ monitors: [monitor] }); expect(listed.headers.get("cache-control")).toBe("no-store");
  expect(fetchMock.mock.calls[1][1]?.method).toBe("GET");
});

it.each([null, {}, { ...input, user_id: "other" }, { ...input, url: "ftp://example.com" }, { ...input, url: "https://u:p@example.com" }, ...[0, -1, 0.5, 2147483648, "7"].map(interval_seconds => ({ ...input, interval_seconds }))])("rejects invalid monitor input %j", async body => {
  expect((await handleMonitors(req(body), "create")).status).toBe(400); expect(fetchMock).not.toHaveBeenCalled();
});

it("requires the configured origin, CSRF header and a session", async () => {
  expect((await handleMonitors(req(input, "", { Origin: "https://evil.example" }), "create")).status).toBe(403);
  expect((await handleMonitors(req(undefined, "", { "X-CSRF-Protection": "" }), "list")).status).toBe(403);
  expect((await handleMonitors(req(input, ""), "create")).status).toBe(401);
  expect(fetchMock).not.toHaveBeenCalled();
});

it("refreshes once before creation and preserves rotated cookies on a failure", async () => {
  const expires = new Date(Date.now() + 86400000).toUTCString();
  fetchMock.mockResolvedValueOnce(new Response(null, { status: 401 }))
    .mockResolvedValueOnce(Response.json({ access_token: "new-access", token_type: "Bearer", expires_in: 900 }, { headers: { "Set-Cookie": `refresh_token=new-refresh; Expires=${expires}; HttpOnly` } }))
    .mockResolvedValueOnce(new Response(null, { status: 500 }));
  const response = await handleMonitors(req(input, "uptime_access=expired; uptime_refresh=valid"), "create");
  expect(response.status).toBe(503); expect(response.cookies.get("uptime_refresh")?.value).toBe("new-refresh");
  expect(fetchMock).toHaveBeenCalledTimes(3);
  expect(new Headers(fetchMock.mock.calls[2][1]?.headers).get("Authorization")).toBe("Bearer new-access");
});

it("does not retry an uncertain creation or clear cookies on a network failure", async () => {
  fetchMock.mockRejectedValueOnce(new Error("network"));
  const response = await handleMonitors(req(input), "create");
  expect(response.status).toBe(503); expect(response.headers.has("set-cookie")).toBe(false); expect(fetchMock).toHaveBeenCalledTimes(1);
});

it("rejects malformed upstream monitor lists", async () => {
  fetchMock.mockResolvedValueOnce(Response.json({ monitors: [{ ...monitor, interval_seconds: -1 }] }));
  expect((await handleMonitors(req(), "list")).status).toBe(502);
});

it("rejects the invalid IPv4 URL before creation, but displays legacy saved rows", async () => {
  expect((await handleMonitors(req({ ...input, url: "https://127.0.0.999" }), "create")).status).toBe(400);
  expect(fetchMock).not.toHaveBeenCalled();
  const legacy = { ...monitor, url: "https://127.0.0.999" };
  fetchMock.mockResolvedValueOnce(Response.json({ monitors: [legacy] }));
  const response = await handleMonitors(req(), "list");
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual({ monitors: [legacy] });
});
it("passes page cursors to Go and strips private fields from each page", async () => {
  fetchMock.mockResolvedValueOnce(Response.json({ monitors: [monitor], next_cursor: "next", private: "secret" }));
  const incoming = req(); incoming.nextUrl.searchParams.set("cursor", "previous");
  const response = await handleMonitors(incoming, "list");
  expect(await response.json()).toEqual({ monitors: [monitor], next_cursor: "next" });
  expect(fetchMock.mock.calls[0][0]).toBe("http://127.0.0.1:8080/api/v1/monitors?cursor=previous");
});
it("limits streamed create bodies before contacting Go", async () => {
  let bytes = 0;
  const cancel = vi.fn();
  const body = new ReadableStream({ pull(controller) { bytes += 4096; controller.enqueue(new Uint8Array(4096)); }, cancel }, { highWaterMark: 0 });
  const incoming = new NextRequest(req(), { body, duplex: "half" } as ConstructorParameters<typeof NextRequest>[1]);
  expect((await handleMonitors(incoming, "create")).status).toBe(413);
  expect(bytes).toBe(20 * 1024); expect(cancel).toHaveBeenCalledOnce();
  expect(fetchMock).not.toHaveBeenCalled();
});
it("retains rotated cookies when a monitor response is malformed", async () => {
  const expires = new Date(Date.now() + 86400000).toUTCString();
  fetchMock.mockResolvedValueOnce(Response.json({ access_token: "new-access", token_type: "Bearer", expires_in: 900 }, { headers: { "Set-Cookie": `refresh_token=new-refresh; Expires=${expires}; HttpOnly` } }))
    .mockResolvedValueOnce(Response.json({ monitors: null }));
  const response = await handleMonitors(req(undefined, "uptime_refresh=valid"), "list");
  expect(response.status).toBe(502);
  expect(response.cookies.get("uptime_refresh")?.value).toBe("new-refresh");
});

const id = "926ccbea-9c77-4b0b-99da-28b94ae35f30";
it("updates only public configuration and acknowledges a bodyless deletion", async () => {
  fetchMock.mockResolvedValueOnce(Response.json({ ...monitor, id, user_id: "private" }));
  const updated = await handleMonitors(req(input), "update", id);
  expect(updated.status).toBe(200); expect(await updated.json()).toEqual({ monitor: { ...monitor, id } });
  expect(fetchMock).toHaveBeenLastCalledWith(`http://127.0.0.1:8080/api/v1/monitors/${id}`, expect.objectContaining({ method: "PUT", body: JSON.stringify(input) }));
  fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
  const deleted = await handleMonitors(req(), "delete", id);
  expect(deleted.status).toBe(200); expect(await deleted.json()).toEqual({ ok: true });
  expect(fetchMock).toHaveBeenLastCalledWith(`http://127.0.0.1:8080/api/v1/monitors/${id}`, expect.objectContaining({ method: "DELETE", body: undefined }));
});
it.each(["update", "delete"] as const)("validates %s IDs and maps missing/foreign rows", async action => {
  expect((await handleMonitors(req(input), action, "../auth/logout")).status).toBe(400);
  expect(fetchMock).not.toHaveBeenCalled();
  fetchMock.mockResolvedValueOnce(new Response(null, { status: 404 }));
  const result = await handleMonitors(req(input), action, id);
  expect(result.status).toBe(404); expect(await result.json()).toMatchObject({ error: { code: "monitor_not_found" } });
});
it("validates update settings before contacting Go", async () => {
  expect((await handleMonitors(req({ ...input, interval_seconds: 0 }), "update", id)).status).toBe(400);
  expect((await handleMonitors(req({ ...input, user_id: "other" }), "update", id)).status).toBe(400);
  expect(fetchMock).not.toHaveBeenCalled();
});
it("rotates cookies during deletion and preserves them on a 404", async () => {
  const expires = new Date(Date.now() + 86400000).toUTCString();
  fetchMock.mockResolvedValueOnce(Response.json({ access_token: "new-access", token_type: "Bearer", expires_in: 900 }, { headers: { "Set-Cookie": `refresh_token=new-refresh; Expires=${expires}; HttpOnly` } }))
    .mockResolvedValueOnce(new Response(null, { status: 404 }));
  const result = await handleMonitors(req(undefined, "uptime_refresh=valid"), "delete", id);
  expect(result.status).toBe(404); expect(result.cookies.get("uptime_refresh")?.value).toBe("new-refresh");
});
it.each(["update", "delete"] as const)("does not retry an uncertain %s", async action => {
  fetchMock.mockRejectedValueOnce(new Error("network"));
  expect((await handleMonitors(req(input), action, id)).status).toBe(503);
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

it("preserves PNG data for create, update and list responses", async () => {
  const iconMonitor = { ...monitor, favicon: "data:image/png;base64,aGVsbG8=" };
  for (const action of ["create", "update", "list"] as const) {
    fetchMock.mockResolvedValueOnce(Response.json(action === "list" ? { monitors: [iconMonitor] } : iconMonitor));
    const response = await handleMonitors(req(action === "list" ? undefined : input), action, id);
    expect(await response.json()).toEqual(action === "list" ? { monitors: [iconMonitor] } : { monitor: iconMonitor });
  }
});

it.each(["https://external.example/icon.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64," + "a".repeat(90000), 123])("discards unsafe or oversized favicon values: %s", async favicon => {
  fetchMock.mockResolvedValueOnce(Response.json({ monitors: [{ ...monitor, favicon }] }));
  const response = await handleMonitors(req(), "list");
  expect(await response.json()).toEqual({ monitors: [monitor] });
});
