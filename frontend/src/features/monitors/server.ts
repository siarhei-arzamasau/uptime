import "server-only";
import { NextRequest } from "next/server";
import { handleAuthenticated } from "../auth/server";
import { APIError } from "../../lib/api-error";
import { readJSON } from "../../lib/request-body";
import { MAX_INTERVAL_SECONDS, validMonitorURL } from "./validation";
import type { Monitor, MonitorPage } from "./types";

function invalidResponse() {
  return new APIError(502, "invalid_response", "The service returned an unexpected monitor. Please try again.");
}
function monitor(value: unknown): Monitor {
  const m = value as Partial<Monitor> | undefined;
  // Validate the response shape without reapplying creation policy to old rows.
  // URLs are displayed as text, so legacy URLs must not break the entire list.
  if (!m || typeof m.id !== "string" || typeof m.url !== "string" || new TextEncoder().encode(m.url).length > 2048 || typeof m.created_at !== "string" || typeof m.interval_seconds !== "number" || !Number.isInteger(m.interval_seconds) || m.interval_seconds < 1 || m.interval_seconds > MAX_INTERVAL_SECONDS) throw invalidResponse();
  return { id: m.id, url: m.url, interval_seconds: m.interval_seconds, created_at: m.created_at };
}
function page(value: unknown): MonitorPage {
  const data = value as Partial<MonitorPage> | undefined;
  if (!data || !Array.isArray(data.monitors) || data.monitors.length > 50 || (data.next_cursor !== undefined && typeof data.next_cursor !== "string")) throw invalidResponse();
  return { monitors: data.monitors.map(monitor), ...(data.next_cursor ? { next_cursor: data.next_cursor } : {}) };
}
function onError(status: number) {
  if (status === 400) return new APIError(400, "invalid_monitor", "Enter a valid HTTP or HTTPS URL, interval or page cursor.");
  if (status === 413) return new APIError(413, "request_too_large", "Request is too large.");
}
export function handleMonitors(req: NextRequest, action: "list" | "create") {
  return handleAuthenticated(req, async () => {
    if (action === "list") {
      const cursor = req.nextUrl.searchParams.get("cursor");
      if (cursor && (cursor.length > 128 || !/^[A-Za-z0-9_-]+$/.test(cursor))) throw new APIError(400, "invalid_cursor", "Invalid page cursor.");
      return { path: `monitors${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, method: "GET", decode: page, onError };
    }
    const body = await readJSON(req, 16 * 1024);
    if (!body || typeof body !== "object" || Array.isArray(body) || Object.keys(body).length !== 2 || !("url" in body) || typeof body.url !== "string" || !("interval_seconds" in body) || typeof body.interval_seconds !== "number") throw new APIError(400, "invalid_request", "Provide only a URL and check interval.");
    const url = body.url.trim(), interval = body.interval_seconds;
    if (!validMonitorURL(url)) throw new APIError(400, "invalid_url", "Enter a valid HTTP or HTTPS URL without credentials or a fragment; use punycode for international domains.");
    if (!Number.isInteger(interval) || interval < 1 || interval > MAX_INTERVAL_SECONDS) throw new APIError(400, "invalid_interval", "Enter a positive whole-number interval up to 2147483647 seconds.");
    return { path: "monitors", method: "POST", body: JSON.stringify({ url, interval_seconds: interval }), decode: data => ({ monitor: monitor(data) }), status: 201, onError };
  });
}
