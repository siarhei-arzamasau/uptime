import type { Action, AuthResult } from "./types";
import type { CreateMonitor, Monitor } from "../monitors/types";

export class AuthError extends Error {
  constructor(message: string, public status: number) { super(message); }
}
let pendingSession: Promise<AuthResult> | undefined;
export function subscribeAuth(listener: () => void) {
  const channel = new BroadcastChannel("uptime-auth");
  channel.onmessage = (event) => { if (event.data === "changed") listener(); };
  return () => channel.close();
}
function notify() {
  const channel = new BroadcastChannel("uptime-auth");
  channel.postMessage("changed"); channel.close();
}
async function request<T = AuthResult>(action: Action, body?: { email: string; password: string } | { name: string } | FormData | CreateMonitor): Promise<T> {
  if (!navigator.locks) throw new AuthError("Please update your browser to securely sign in.", 0);
  return navigator.locks.request("uptime-auth", async () => {
    let res: Response;
    try {
      res = await fetch(`/api/auth/${action === "profile-update" ? "profile" : action === "avatar" ? "profile/avatar" : action === "monitor-create" ? "monitors/create" : action}`, {
        method: action === "profile-update" ? "PATCH" : "POST", credentials: "same-origin", cache: "no-store",
        headers: body instanceof FormData ? { "X-CSRF-Protection": "1" } : { "Content-Type": "application/json", "X-CSRF-Protection": "1" },
        body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined,
      });
    } catch { throw new AuthError("Connection interrupted. Please try again.", 0); }
    const data = await res.json();
    if (!res.ok) throw new AuthError(data.error?.message ?? "Something went wrong. Please try again.", res.status);
    if (action === "login" || action === "register" || action === "logout") notify();
    return data;
  });
}
export function session(): Promise<AuthResult> {
  if (!pendingSession) pendingSession = request("session").finally(() => { pendingSession = undefined; });
  return pendingSession;
}
export function signIn(action: "login" | "register", email: string, password: string) { return request(action, { email, password }); }
export function signOut() { return request("logout"); }

export function loadProfile() { return request("profile"); }
export function updateProfile(name: string) { return request("profile-update", { name }); }
export function loadMonitors() { return request<{ monitors: Monitor[] }>("monitors"); }
export function createMonitor(body: CreateMonitor) { return request<{ monitor: Monitor }>("monitor-create", body); }

export function uploadAvatar(name: string, file: File) {
  const form = new FormData(); form.set("name", name); form.set("avatar", file);
  return request("avatar", form);
}
