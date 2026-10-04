import type { Action, AuthResult } from "./types";
import { authenticatedRequest } from "./transport";
export { AuthError } from "./transport";

let pendingSession: Promise<AuthResult> | undefined;
/**
 * Subscribes to sign-in/out changes from other tabs and returns an unsubscribe callback.
 * Requires BroadcastChannel; local mutations notify peers only after a successful request.
 */
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
/**
 * Returns the { user } result, sharing an in-flight request among callers in this tab.
 * May rotate cookies; rejections follow authenticatedRequest and clear the pending cache.
 */
export function session(): Promise<AuthResult> {
  if (!pendingSession) pendingSession = request("session").finally(() => { pendingSession = undefined; });
  return pendingSession;
}
/**
 * Returns { user } after sign-in, keeping tokens in HttpOnly cookies and notifying other tabs.
 * Credential, capacity, and transport failures reject as in authenticatedRequest.
 */
export function signIn(action: "login" | "register", email: string, password: string) { return request(action, { email, password }); }
/**
 * Returns the logout acknowledgement after revocation and cookie clearing, then notifies peers.
 * Request failures reject as in authenticatedRequest without emitting an auth-change event.
 */
export function signOut() { return request("logout"); }

/** Returns { user } for the current profile, refreshing cookies if needed; failures reject as in authenticatedRequest. */
export function loadProfile() { return request("profile"); }
/**
 * Returns { user } after a name-only update; the avatar is preserved.
 * Validation, session, and transport failures reject as in authenticatedRequest.
 */
export function updateProfile(name: string) { return request("profile-update", { name }); }

/**
 * Returns { user } after saving name and avatar together; no preview URL is persisted.
 * Invalid uploads, session failures, and transport errors reject as in authenticatedRequest.
 */
export function uploadAvatar(name: string, file: File) {
  const form = new FormData(); form.set("name", name); form.set("avatar", file);
  return request("avatar", form);
}
