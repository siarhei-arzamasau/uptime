import type { Action, AuthResult } from "./types";
import { authenticatedRequest } from "./transport";
export { AuthError } from "./transport";

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
async function request(action: Action, body?: { email: string; password: string } | { name: string } | FormData): Promise<AuthResult> {
  const path = action === "profile-update" ? "profile" : action === "avatar" ? "profile/avatar" : action;
  const data = await authenticatedRequest<AuthResult>(path, body, action === "profile-update" ? "PATCH" : "POST");
  if (action === "login" || action === "register" || action === "logout") notify();
  return data;
}
export function session(): Promise<AuthResult> {
  if (!pendingSession) pendingSession = request("session").finally(() => { pendingSession = undefined; });
  return pendingSession;
}
export function signIn(action: "login" | "register", email: string, password: string) { return request(action, { email, password }); }
export function signOut() { return request("logout"); }

export function loadProfile() { return request("profile"); }
export function updateProfile(name: string) { return request("profile-update", { name }); }

export function uploadAvatar(name: string, file: File) {
  const form = new FormData(); form.set("name", name); form.set("avatar", file);
  return request("avatar", form);
}
