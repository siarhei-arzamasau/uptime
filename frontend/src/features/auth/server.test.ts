import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { handleAuth } from "./server";
const u = { id: "user-id", email: "person@example.com", created_at: "2026-01-01" };
const expires = new Date(Date.now() + 86_400_000).toUTCString();
function tokenReply() { return Response.json({ user: { ...u, password_hash: "secret" }, access_token: "jwt-secret", token_type: "Bearer", expires_in: 900 }, { headers: { "Set-Cookie": `refresh_token=refresh-secret; Path=/api/v1/auth; Expires=${expires}; HttpOnly` } }); }
function req(cookie = "", body?: unknown, headers = {}) { return new NextRequest("http://localhost:3000/api/auth/session", { method: "POST", headers: { Origin: "http://localhost:3000", "X-CSRF-Protection": "1", "Content-Type": "application/json", Cookie: cookie, ...headers }, body: body ? JSON.stringify(body) : undefined }); }
const fetchMock = vi.fn<typeof fetch>();
beforeEach(() => { vi.stubGlobal("fetch", fetchMock); fetchMock.mockReset(); vi.stubEnv("APP_ORIGIN", "http://localhost:3000"); });
afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs(); });
describe("server session boundary", () => {
  it.each(["login", "register"] as const)("%s sets HttpOnly tokens without exposing them", async action => {
    vi.stubEnv("NODE_ENV", "production"); fetchMock.mockResolvedValueOnce(tokenReply());
    const res = await handleAuth(req("", { email: u.email, password: "password-long", confirm: "ignored" }), action);
    expect(await res.json()).toEqual({ user: u });
    expect(res.cookies.get("uptime_access")).toMatchObject({ value: "jwt-secret", httpOnly: true, secure: true, sameSite: "lax", path: "/", maxAge: 900 });
    expect(res.cookies.get("uptime_refresh")).toMatchObject({ httpOnly: true, path: "/api/auth", expires: new Date(expires) });
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).not.toHaveProperty("confirm");
  });
  it("uses valid access without rotating", async () => {
    fetchMock.mockResolvedValueOnce(Response.json(u));
    const res = await handleAuth(req("uptime_access=valid; uptime_refresh=refresh"), "session");
    expect(await res.json()).toEqual({ user: u }); expect(fetchMock).toHaveBeenCalledTimes(1); expect(res.cookies.getAll()).toHaveLength(0);
  });
  it.each(["", "uptime_access=expired;"])("refreshes missing or expired access once: %s", async access => {
    if (access) fetchMock.mockResolvedValueOnce(new Response(null, { status: 401 }));
    fetchMock.mockResolvedValueOnce(tokenReply()).mockResolvedValueOnce(Response.json(u));
    const res = await handleAuth(req(`${access} uptime_refresh=refresh`), "session");
    expect(res.status).toBe(200); expect(res.cookies.get("uptime_refresh")?.value).toBe("refresh-secret");
    expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/refresh"))).toHaveLength(1);
  });
  it("does not loop after a refreshed JWT is rejected", async () => {
    fetchMock.mockResolvedValueOnce(tokenReply()).mockResolvedValueOnce(new Response(null, { status: 401 }));
    const res = await handleAuth(req("uptime_refresh=refresh"), "session"); expect(res.status).toBe(401); expect(fetchMock).toHaveBeenCalledTimes(2); expect(res.cookies.get("uptime_refresh")?.maxAge).toBe(0);
  });
  it("clears both cookies when refresh is invalid", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 401 })); const res = await handleAuth(req("uptime_refresh=bad"), "session"); expect(res.status).toBe(401); expect(res.cookies.getAll().every(c => c.maxAge === 0)).toBe(true);
  });
  it.each([500, 503])("preserves cookies on backend %i", async status => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status })); const res = await handleAuth(req("uptime_access=valid; uptime_refresh=refresh"), "session"); expect(res.status).toBe(503); expect(res.cookies.getAll()).toHaveLength(0); expect(fetchMock).toHaveBeenCalledTimes(1);
  });
  it("preserves cookies on a connection error", async () => {
    fetchMock.mockRejectedValueOnce(new Error("private backend URL")); const res = await handleAuth(req("uptime_refresh=refresh"), "session"); expect(res.status).toBe(503); expect(res.cookies.getAll()).toHaveLength(0); expect(await res.text()).not.toContain("private backend");
  });
  it("keeps a completed rotation when me subsequently fails", async () => {
    fetchMock.mockResolvedValueOnce(tokenReply()).mockResolvedValueOnce(new Response(null, { status: 503 })); const res = await handleAuth(req("uptime_refresh=refresh"), "session"); expect(res.status).toBe(503); expect(res.cookies.get("uptime_refresh")?.value).toBe("refresh-secret");
  });
  it.each([{ Origin: "https://evil.example" }, { "X-CSRF-Protection": "" }])("rejects invalid request origin/protection", async headers => {
    const res = await handleAuth(req("", undefined, headers), "logout"); expect(res.status).toBe(403); expect(fetchMock).not.toHaveBeenCalled();
  });
  it.each([[401, "Incorrect email"], [409, "already exists"], [400, "valid email"]])("maps login error %i", async (status, text) => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: Number(status) })); const res = await handleAuth(req("", { email: "x", password: "x" }), "login"); expect(res.status).toBe(status); expect(await res.text()).toContain(text);
  });
  it("logout clears only after confirmed revocation", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 503 })).mockResolvedValueOnce(new Response(null, { status: 204 }));
    const first = await handleAuth(req("uptime_refresh=refresh"), "logout"); expect(first.cookies.getAll()).toHaveLength(0);
    const second = await handleAuth(req("uptime_refresh=refresh"), "logout"); expect(second.cookies.getAll()).toHaveLength(2); expect(second.cookies.getAll().every(c => c.maxAge === 0)).toBe(true);
  });
  it("rejects malformed successful backend responses", async () => {
    fetchMock.mockResolvedValueOnce(Response.json({ access_token: "secret" })); const res = await handleAuth(req("", { email: "x", password: "x" }), "login"); expect(res.status).toBe(502); expect(await res.text()).not.toContain("secret");
  });
});
