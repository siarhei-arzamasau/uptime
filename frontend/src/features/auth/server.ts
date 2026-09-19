import "server-only";
import { NextRequest, NextResponse } from "next/server";
import type { Action, User } from "./types";

const ACCESS = "uptime_access";
const REFRESH = "uptime_refresh";
class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}
function user(value: unknown): User {
  const u = value as Partial<User> | undefined;
  if (!u || typeof u.id !== "string" || typeof u.email !== "string" || typeof u.created_at !== "string") {
    throw new APIError(502, "invalid_response", "The service returned an unexpected response. Please try again.");
  }
  return { id: u.id, email: u.email, created_at: u.created_at };
}
function response(body: unknown, status = 200) {
  return NextResponse.json(body, { status, headers: { "Cache-Control": "no-store", "Vary": "Cookie", "X-Content-Type-Options": "nosniff" } });
}
function clear(res: NextResponse, secure: boolean) {
  for (const [name, path] of [[ACCESS, "/"], [REFRESH, "/api/auth"]]) {
    res.cookies.set(name, "", { httpOnly: true, secure, sameSite: "lax", path, maxAge: 0 });
  }
}
function tokens(body: Record<string, unknown>, upstream: Response) {
  if (typeof body.access_token !== "string" || body.expires_in !== 900 || body.token_type !== "Bearer") {
    throw new APIError(502, "invalid_response", "Unable to establish your session. Please sign in again.");
  }
  const cookie = upstream.headers.getSetCookie().find((c) => c.startsWith("refresh_token="));
  const value = cookie?.match(/^refresh_token=([A-Za-z0-9_-]+);/i)?.[1];
  const expires = cookie?.match(/;\s*Expires=([^;]+)/i)?.[1];
  const date = expires ? new Date(expires) : null;
  if (!value || !date || !Number.isFinite(date.getTime()) || date.getTime() <= Date.now()) {
    throw new APIError(502, "invalid_response", "Unable to establish your session. Please sign in again.");
  }
  return { access: body.access_token, refresh: value, expires: date };
}
function setTokens(res: NextResponse, pair: ReturnType<typeof tokens>, secure: boolean) {
  const common = { httpOnly: true, secure, sameSite: "lax" as const };
  res.cookies.set(ACCESS, pair.access, { ...common, path: "/", maxAge: 900 });
  // Preserve the backend's absolute expiry, not another 30 days from now.
  res.cookies.set(REFRESH, pair.refresh, { ...common, path: "/api/auth", expires: pair.expires });
}
export async function handleAuth(req: NextRequest, action: Action) {
  const origin = process.env.APP_ORIGIN ?? "http://localhost:3000";
  const secure = process.env.NODE_ENV === "production";
  if (req.headers.get("origin") !== origin || req.headers.get("x-csrf-protection") !== "1") {
    return response({ error: { code: "forbidden", message: "This request is not allowed." } }, 403);
  }
  const backend = process.env.BACKEND_URL ?? "http://127.0.0.1:8080";
  async function call(path: string, init: RequestInit = {}) {
    let result: Response;
    try {
      result = await fetch(`${backend}/api/v1/auth/${path}`, {
        ...init, cache: "no-store", redirect: "error", signal: AbortSignal.timeout(10_000),
        headers: { "Content-Type": "application/json", "X-CSRF-Protection": "1", ...init.headers },
      });
    } catch {
      throw new APIError(503, "unavailable", "We couldn’t reach the service. Please try again.");
    }
    if (!result.ok) {
      if (result.status === 401) throw new APIError(401, "unauthorized", action === "login" ? "Incorrect email or password." : "Your session has ended. Please sign in again.");
      if (result.status === 409) throw new APIError(409, "email_exists", "An account with this email already exists. Sign in instead.");
      if (result.status === 400) throw new APIError(400, "invalid_request", "Enter a valid email and a password of 12–128 characters.");
      throw new APIError(503, "unavailable", "The service is temporarily unavailable. Please try again.");
    }
    return result;
  }
  const access = req.cookies.get(ACCESS)?.value;
  const refresh = req.cookies.get(REFRESH)?.value;
  const refreshHeaders: Record<string, string> = refresh ? { Cookie: `refresh_token=${encodeURIComponent(refresh)}` } : {};
  try {
    if (action === "login" || action === "register") {
      if (!req.headers.get("content-type")?.startsWith("application/json")) throw new APIError(400, "invalid_request", "Expected a JSON request.");
      const raw = await req.text();
      if (raw.length > 4096) throw new APIError(400, "invalid_request", "Request is too large.");
      let body: { email?: unknown; password?: unknown };
      try { body = JSON.parse(raw); } catch { throw new APIError(400, "invalid_request", "Invalid request."); }
      if (!body || typeof body.email !== "string" || typeof body.password !== "string") throw new APIError(400, "invalid_request", "Email and password are required.");
      const upstream = await call(action, { method: "POST", body: JSON.stringify({ email: body.email, password: body.password }) });
      const data = await upstream.json();
      const pair = tokens(data, upstream);
      const res = response({ user: user(data.user) }, action === "register" ? 201 : 200);
      setTokens(res, pair, secure);
      return res;
    }
    if (action === "logout") {
      await call("logout", { method: "POST", headers: refreshHeaders });
      const res = response({ ok: true }); clear(res, secure); return res;
    }
    if (access) {
      try {
        const me = await call("me", { headers: { Authorization: `Bearer ${access}` } });
        return response({ user: user(await me.json()) });
      } catch (error) { if (!(error instanceof APIError) || error.status !== 401) throw error; }
    }
    if (!refresh) throw new APIError(401, "unauthorized", "Please sign in to continue.");
    const upstream = await call("refresh", { method: "POST", headers: refreshHeaders });
    const pair = tokens(await upstream.json(), upstream);
    // Persist a successful rotation even if the following /me request fails transiently.
    try {
      const me = await call("me", { headers: { Authorization: `Bearer ${pair.access}` } });
      const res = response({ user: user(await me.json()) }); setTokens(res, pair, secure); return res;
    } catch (error) {
      if (error instanceof APIError && error.status !== 401) {
        const res = response({ error: { code: error.code, message: error.message } }, error.status);
        setTokens(res, pair, secure); return res;
      }
      throw error;
    }
  } catch (error) {
    const e = error instanceof APIError ? error : new APIError(502, "invalid_response", "The service returned an unexpected response. Please try again.");
    const res = response({ error: { code: e.code, message: e.message } }, e.status);
    if (action === "session" && e.status === 401) clear(res, secure);
    return res;
  }
}
