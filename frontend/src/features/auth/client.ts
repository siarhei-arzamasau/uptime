import type { Action, AuthResult } from "./types";

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
async function request(action: Action, body?: { email: string; password: string }): Promise<AuthResult> {
  if (!navigator.locks) throw new AuthError("Please update your browser to securely sign in.", 0);
  return navigator.locks.request("uptime-auth", async () => {
    let res: Response;
    try {
      res = await fetch(`/api/auth/${action}`, {
        method: "POST", credentials: "same-origin", cache: "no-store",
        headers: { "Content-Type": "application/json", "X-CSRF-Protection": "1" },
        body: body ? JSON.stringify(body) : undefined,
      });
    } catch { throw new AuthError("Connection interrupted. Please try again.", 0); }
    const data = await res.json();
    if (!res.ok) throw new AuthError(data.error?.message ?? "Something went wrong. Please try again.", res.status);
    if (action !== "session") notify();
    return data;
  });
}
export function session(): Promise<AuthResult> {
  if (!pendingSession) pendingSession = request("session").finally(() => { pendingSession = undefined; });
  return pendingSession;
}
export function signIn(action: "login" | "register", email: string, password: string) { return request(action, { email, password }); }
export function signOut() { return request("logout"); }
