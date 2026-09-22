import "server-only";
import { NextRequest, NextResponse } from "next/server";
import type { Action, User } from "./types";
import { readBody, readJSON } from "../../lib/request-body";
import { APIError } from "../../lib/api-error";

const ACCESS = "uptime_access";
const REFRESH = "uptime_refresh";
function user(value: unknown): User {
  const u = value as Partial<User> | undefined;
  if (!u || typeof u.id !== "string" || typeof u.email !== "string" || typeof u.name !== "string" || typeof u.avatar_url !== "string" || typeof u.created_at !== "string") {
    throw new APIError(502, "invalid_response", "The service returned an unexpected response. Please try again.");
  }
  if (u.avatar_url && !/^\/api\/v1\/avatars\/[0-9a-f-]{36}\.(png|jpg)$/.test(u.avatar_url)) throw new APIError(502, "invalid_response", "Invalid avatar response.");
  return { avatar_url: u.avatar_url.replace("/api/v1/avatars/", "/api/avatars/"), id: u.id, email: u.email, name: u.name, created_at: u.created_at };
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
export type AuthenticatedOperation = {
  path: string;
  method: "GET" | "POST";
  body?: string;
  decode: (data: unknown) => unknown;
  status?: number;
  onError?: (status: number) => APIError | undefined;
};

// Feature handlers supply their operation; token rotation and Go access stay here.
export function handleAuthenticated(req: NextRequest, prepare: () => Promise<AuthenticatedOperation>) {
  return handleRequest(req, undefined, prepare);
}

export function handleAuth(req: NextRequest, action: Action) {
  return handleRequest(req, action);
}

async function handleRequest(req: NextRequest, action?: Action, prepare?: () => Promise<AuthenticatedOperation>) {
  const origin = process.env.APP_ORIGIN ?? "http://localhost:3000";
  const secure = process.env.NODE_ENV === "production";
  if (req.headers.get("origin") !== origin || req.headers.get("x-csrf-protection") !== "1") {
    return response({ error: { code: "forbidden", message: "This request is not allowed." } }, 403);
  }
  const backend = process.env.BACKEND_URL ?? "http://127.0.0.1:8080";
  async function call(path: string, init: RequestInit = {}, onError?: AuthenticatedOperation["onError"]) {
    let result: Response;
    try {
      const headers = new Headers(init.headers);
      headers.set("X-CSRF-Protection", "1");
      if (!(init.body instanceof FormData)) headers.set("Content-Type", "application/json");
      result = await fetch(`${backend}/api/v1/${path}`, {
        ...init, cache: "no-store", redirect: "error", signal: AbortSignal.timeout(10_000),
        headers,
      });
    } catch {
      throw new APIError(503, "unavailable", "We couldn’t reach the service. Please try again.");
    }
    if (!result.ok) {
      const domainError = onError?.(result.status);
      if (domainError) throw domainError;
      if (result.status === 401) throw new APIError(401, "unauthorized", action === "login" ? "Incorrect email or password." : "Your session has ended. Please sign in again.");
      if (result.status === 409 && (action === "login" || action === "register")) throw new APIError(409, "email_exists", "An account with this email already exists. Sign in instead.");
      if (result.status === 413 && action === "avatar") throw new APIError(413, "avatar_too_large", "Choose an image under 5 MB.");
      if (result.status === 400 && action === "avatar") throw new APIError(400, "invalid_avatar", "Choose a valid JPEG or PNG up to 2048 × 2048 pixels.");
      if (result.status === 400 && action) throw new APIError(400, "invalid_request", action === "profile-update" ? "Name must be at most 100 characters without control characters." : "Enter a valid email and a password of 12–128 characters.");
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
      const body = await readJSON(req, 4096);
      if (!body || typeof body !== "object" || !("email" in body) || !("password" in body) || typeof body.email !== "string" || typeof body.password !== "string") throw new APIError(400, "invalid_request", "Email and password are required.");
      const upstream = await call(`auth/${action}`, { method: "POST", body: JSON.stringify({ email: body.email, password: body.password }) });
      const data = await upstream.json();
      const pair = tokens(data, upstream);
      const res = response({ user: user(data.user) }, action === "register" ? 201 : 200);
      setTokens(res, pair, secure);
      return res;
    }
    if (action === "logout") {
      await call("auth/logout", { method: "POST", headers: refreshHeaders });
      const res = response({ ok: true }); clear(res, secure); return res;
    }
    let profileBody: string | FormData | undefined;
    const operation = await prepare?.();
    if (action === "avatar") {
      if (!req.headers.get("content-type")?.startsWith("multipart/form-data;")) throw new APIError(400, "invalid_request", "Expected an image upload.");
      profileBody = await avatarForm(req);
    }
    if (action === "profile-update") {
      if (!req.headers.get("content-type")?.startsWith("application/json")) throw new APIError(400, "invalid_request", "Expected a JSON request.");
      const body = await readJSON(req, 4096);
      if (!body || typeof body !== "object" || Array.isArray(body) || Object.keys(body).length !== 1 || !("name" in body) || typeof body.name !== "string") throw new APIError(400, "invalid_request", "Provide only the name field as a string.");
      const name = body.name.trim();
      if ([...name].length > 100 || /\p{Cc}/u.test(name)) throw new APIError(400, "invalid_name", "Name must be at most 100 characters without control characters.");
      profileBody = JSON.stringify({ name });
    }
    const protectedRequest = (token: string) => call(operation?.path ?? (action === "session" ? "auth/me" : action === "avatar" ? "profile/avatar" : "profile"), {
      method: operation?.method ?? (action === "profile-update" ? "PATCH" : action === "avatar" ? "POST" : "GET"),
      headers: { Authorization: `Bearer ${token}` }, body: operation?.body ?? profileBody,
    }, operation?.onError);
    async function protectedResponse(upstream: Response) {
      let data: unknown;
      try { data = await upstream.json(); }
      catch { throw new APIError(502, "invalid_response", "The service returned an unexpected response. Please try again."); }
      return operation ? response(operation.decode(data), operation.status) : response({ user: user(data) });
    }
    if (access) {
      try {
        const me = await protectedRequest(access);
        return await protectedResponse(me);
      } catch (error) { if (!(error instanceof APIError) || error.status !== 401) throw error; }
    }
    if (!refresh) throw new APIError(401, "unauthorized", "Please sign in to continue.");
    const upstream = await call("auth/refresh", { method: "POST", headers: refreshHeaders });
    const pair = tokens(await upstream.json(), upstream);
    // Persist a successful rotation even if the protected operation fails.
    try {
      const me = await protectedRequest(pair.access);
      const res = await protectedResponse(me); setTokens(res, pair, secure); return res;
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
    if ((prepare || (action && ["session", "profile", "profile-update", "avatar"].includes(action))) && e.status === 401) clear(res, secure);
    return res;
  }
}

// Enforce the upload limit while reading, including clients without Content-Length.
async function avatarForm(req: NextRequest): Promise<FormData> {
  const limit = 5 * 1024 * 1024 + 64 * 1024;
  const bytes = await readBody(req, limit, new APIError(413, "avatar_too_large", "Choose an image under 5 MB."));
  let form: FormData;
  try { form = await new Response(bytes, { headers: { "Content-Type": req.headers.get("content-type")! } }).formData(); }
  catch { throw new APIError(400, "invalid_request", "Invalid image upload."); }
  const file = form.get("avatar"), name = form.get("name");
  if (!(file instanceof File) || typeof name !== "string" || [...form.keys()].length !== 2 || form.getAll("avatar").length !== 1 || form.getAll("name").length !== 1) throw new APIError(400, "invalid_request", "Provide only name and one avatar file.");
  if (file.size > 5 * 1024 * 1024) throw new APIError(413, "avatar_too_large", "Choose an image under 5 MB.");
  if (!["image/png", "image/jpeg"].includes(file.type) || !file.size) throw new APIError(400, "invalid_avatar", "Choose a JPEG or PNG image.");
  if ([...name.trim()].length > 100 || /\p{Cc}/u.test(name.trim())) throw new APIError(400, "invalid_name", "Name must be at most 100 characters without control characters.");
  form.set("name", name.trim());
  return form;
}

export async function serveAvatar(filename: string) {
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|jpg)$/.test(filename)) return new Response(null, { status: 404 });
  try {
    const upstream = await fetch(`${process.env.BACKEND_URL ?? "http://127.0.0.1:8080"}/api/v1/avatars/${filename}`, { cache: "no-store", redirect: "error", signal: AbortSignal.timeout(10_000) });
    if (!upstream.ok) return new Response(null, { status: upstream.status === 404 ? 404 : 503 });
    const type = upstream.headers.get("content-type");
    if (type !== "image/png" && type !== "image/jpeg") return new Response(null, { status: 502 });
    return new Response(upstream.body, { headers: { "Content-Type": type, "Cache-Control": "public, max-age=31536000, immutable", "X-Content-Type-Options": "nosniff", "Content-Disposition": "inline" } });
  } catch { return new Response(null, { status: 503 }); }
}
