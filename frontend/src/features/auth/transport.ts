/** An HTTP/session failure, with status 0 for network or unsupported-browser failures. */
export class AuthError extends Error {
  constructor(message: string, public status: number) { super(message); }
}

/**
 * Returns decoded BFF JSON while serializing cookie changes across tabs with Web Locks.
 * Rejects with AuthError for unsupported locks, network failures, or non-success HTTP
 * responses; JSON parsing and lock API failures propagate. No caller abort signal is used.
 */
export async function authenticatedRequest<T>(path: string, body?: object | FormData, method = "POST"): Promise<T> {
  if (!navigator.locks) throw new AuthError("Please update your browser to securely sign in.", 0);
  return navigator.locks.request("uptime-auth", async () => {
    let res: Response;
    try {
      res = await fetch(`/api/auth/${path}`, {
        method, credentials: "same-origin", cache: "no-store",
        headers: body instanceof FormData ? { "X-CSRF-Protection": "1" } : { "Content-Type": "application/json", "X-CSRF-Protection": "1" },
        body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined,
      });
    } catch { throw new AuthError("Connection interrupted. Please try again.", 0); }
    const data = await res.json();
    if (!res.ok) throw new AuthError(data.error?.message ?? "Something went wrong. Please try again.", res.status);
    return data;
  });
}
