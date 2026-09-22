import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { handleAuth } from "./server";
const u = { id: "user-id", email: "person@example.com", name: "", avatar_url: "", created_at: "2026-01-01" };
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
  it.each(["login", "register"] as const)("preserves cookies and retry guidance when %s is throttled", async action => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 429, headers: { "Retry-After": "1" } }));
    const res = await handleAuth(req("uptime_access=existing; uptime_refresh=existing", { email: u.email, password: "correct long password" }), action);
    expect(res.status).toBe(429);
    expect(res.headers.get("retry-after")).toBe("1");
    expect(await res.json()).toMatchObject({ error: { code: "auth_busy", message: expect.stringContaining("wait a moment") } });
    expect(res.cookies.getAll()).toHaveLength(0);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
  it("rejects malformed successful backend responses", async () => {
    fetchMock.mockResolvedValueOnce(Response.json({ access_token: "secret" })); const res = await handleAuth(req("", { email: "x", password: "x" }), "login"); expect(res.status).toBe(502); expect(await res.text()).not.toContain("secret");
  });
});

describe("profile boundary", () => {
  it("reads the private profile without leaking extra fields", async () => {
    fetchMock.mockResolvedValueOnce(Response.json({ ...u, name: "Alice", password_hash: "secret" }));
    const res = await handleAuth(req("uptime_access=valid"), "profile");
    expect(await res.json()).toEqual({ user: { ...u, name: "Alice" } });
    expect(fetchMock.mock.calls[0][0]).toBe("http://127.0.0.1:8080/api/v1/profile");
    expect(res.headers.get("cache-control")).toBe("no-store");
  });
  it("updates only name and retries with a renewed access token", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 401 })).mockResolvedValueOnce(tokenReply()).mockResolvedValueOnce(Response.json({ ...u, name: "Alice" }));
    const res = await handleAuth(req("uptime_access=expired; uptime_refresh=valid", { name: " Alice " }), "profile-update");
    expect(res.status).toBe(200); expect(await res.json()).toEqual({ user: { ...u, name: "Alice" } });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls[2][1]).toMatchObject({ method: "PATCH", body: JSON.stringify({ name: "Alice" }), headers: expect.any(Headers) });
    expect(new Headers(fetchMock.mock.calls[2][1]?.headers).get("Authorization")).toBe("Bearer jwt-secret");
    expect(res.cookies.get("uptime_refresh")?.value).toBe("refresh-secret");
  });
  it.each([{ name: "A", email: "other@example.com" }, { name: null }, {}, { name: "x".repeat(101) }, { name: "a\nb" }])("rejects unsupported profile input %j", async body => {
    const res = await handleAuth(req("uptime_access=valid", body), "profile-update");
    expect(res.status).toBe(400); expect(fetchMock).not.toHaveBeenCalled();
  });
  it.each(["profile", "profile-update"] as const)("protects %s with origin and CSRF", async action => {
    for (const headers of [{ Origin: "https://evil.example" }, { "X-CSRF-Protection": "" }]) {
      const res = await handleAuth(req("uptime_access=valid", { name: "A" }, headers), action);
      expect(res.status).toBe(403);
    }
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("keeps rotated cookies after an update failure and allows retry", async () => {
    fetchMock.mockResolvedValueOnce(tokenReply()).mockResolvedValueOnce(new Response(null, { status: 500 }));
    const res = await handleAuth(req("uptime_refresh=valid", { name: "A" }), "profile-update");
    expect(res.status).toBe(503); expect(res.cookies.get("uptime_refresh")?.value).toBe("refresh-secret");
  });
  it("clears an invalid profile session", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 401 }));
    const res = await handleAuth(req("uptime_refresh=invalid", { name: "A" }), "profile-update");
    expect(res.status).toBe(401); expect(res.cookies.getAll().every(c => c.maxAge === 0)).toBe(true);
  });
});

describe("avatar upload boundary", () => {
  function uploadReq(file = new File(["png bytes"], "avatar.png", { type: "image/png" }), cookie = "uptime_access=valid", headers = {}) {
    const form = new FormData(); form.set("name", " Alice "); form.set("avatar", file);
    return new NextRequest("http://localhost:3000/api/auth/profile/avatar", { method: "POST", headers: { Origin: "http://localhost:3000", "X-CSRF-Protection": "1", Cookie: cookie, ...headers }, body: form });
  }
  const avatar = "/api/v1/avatars/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.png";
  it("forwards multipart and translates the image URL without exposing tokens", async () => {
    fetchMock.mockResolvedValueOnce(Response.json({ ...u, avatar_url: avatar }));
    const res = await handleAuth(uploadReq(), "avatar");
    expect(res.status).toBe(200); expect(await res.json()).toEqual({ user: { ...u, avatar_url: avatar.replace("/api/v1", "/api") } });
    const [url, init] = fetchMock.mock.calls[0]; expect(url).toBe("http://127.0.0.1:8080/api/v1/profile/avatar");
    expect(init?.body).toBeInstanceOf(FormData); expect((init?.body as FormData).get("name")).toBe("Alice");
    expect(new Headers(init?.headers).get("content-type")).toBeNull();
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer valid");
  });
  it("replays the buffered upload once after JWT refresh", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 401 })).mockResolvedValueOnce(tokenReply()).mockResolvedValueOnce(Response.json({ ...u, avatar_url: avatar }));
    const res = await handleAuth(uploadReq(undefined, "uptime_access=expired; uptime_refresh=valid"), "avatar");
    expect(res.status).toBe(200); expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls[2][1]?.body).toBe(fetchMock.mock.calls[0][1]?.body);
    expect(res.cookies.get("uptime_refresh")?.value).toBe("refresh-secret");
  });
  it("rejects unsupported types and large bodies before calling Go", async () => {
    expect((await handleAuth(uploadReq(new File(["svg"], "a.svg", { type: "image/svg+xml" })), "avatar")).status).toBe(400);
    expect((await handleAuth(uploadReq(undefined, "", { "Content-Length": String(6 * 1024 * 1024) }), "avatar")).status).toBe(413);
    const huge = new File([new Uint8Array(6 * 1024 * 1024)], "large.png", { type: "image/png" });
    const encoded = uploadReq(huge);
    const incoming = new NextRequest(encoded.url, { method: "POST", headers: encoded.headers, body: await encoded.arrayBuffer() });
    expect((await handleAuth(incoming, "avatar")).status).toBe(413);
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("checks CSRF and rejects invalid sessions", async () => {
    expect((await handleAuth(uploadReq(undefined, "", { Origin: "https://evil.example" }), "avatar")).status).toBe(403);
    expect((await handleAuth(uploadReq(undefined, "", { "X-CSRF-Protection": "" }), "avatar")).status).toBe(403);
    expect((await handleAuth(uploadReq(undefined, ""), "avatar")).status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("retains rotated cookies if upload storage fails", async () => {
    fetchMock.mockResolvedValueOnce(tokenReply()).mockResolvedValueOnce(new Response(null, { status: 500 }));
    const res = await handleAuth(uploadReq(undefined, "uptime_refresh=valid"), "avatar");
    expect(res.status).toBe(503); expect(res.cookies.get("uptime_refresh")?.value).toBe("refresh-secret");
  });
});
