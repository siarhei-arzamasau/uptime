import "server-only";
import { NextRequest } from "next/server";
import { handleAuthenticated } from "../auth/server";
import { APIError } from "../../lib/api-error";
import type { HistoryBucket, MonitorCheck, MonitorHistory } from "./types";

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const timestamp = (value: unknown): value is string => typeof value === "string" && Number.isFinite(Date.parse(value));
const count = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
const percentage = (value: unknown): value is number | null => value === null || (typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 100);
function invalid() { return new APIError(502, "invalid_response", "The service returned unexpected monitoring data."); }

/** Decodes only public status fields; rejects malformed upstream values. */
export function decodeCheck(value: unknown): MonitorCheck {
  const c = value as Partial<MonitorCheck> | null;
  if (!c || typeof c.id !== "string" || !count(c.version) || c.version < 1 || !["pending", "up", "down", "stale"].includes(c.status ?? "") || typeof c.error_kind !== "string") throw invalid();
  if ((c.last_started_at !== null && !timestamp(c.last_started_at)) || (c.last_finished_at !== null && !timestamp(c.last_finished_at)) || (c.last_success !== null && typeof c.last_success !== "boolean") || (c.http_status !== null && (!count(c.http_status) || c.http_status < 100 || c.http_status > 599))) throw invalid();
  return { id: c.id, version: c.version, status: c.status as MonitorCheck["status"], last_started_at: c.last_started_at, last_finished_at: c.last_finished_at, last_success: c.last_success, http_status: c.http_status, error_kind: c.error_kind };
}

function decodeHistory(value: unknown): MonitorHistory {
  const h = value as Partial<MonitorHistory> | null;
  if (!h || !timestamp(h.from) || !timestamp(h.to) || !count(h.version) || h.version < 1 || ![60, 300, 1800, 7200].includes(h.step_seconds ?? 0) || !count(h.successes) || !count(h.failures) || !percentage(h.availability) || !Array.isArray(h.buckets) || h.buckets.length > 361) throw invalid();
  const buckets = h.buckets.map((b: HistoryBucket) => {
    if (!b || !timestamp(b.start) || !timestamp(b.end) || !count(b.successes) || !count(b.failures) || !percentage(b.availability)) throw invalid();
    return { start: b.start, end: b.end, successes: b.successes, failures: b.failures, availability: b.availability };
  });
  return { from: h.from, to: h.to, version: h.version, step_seconds: h.step_seconds!, successes: h.successes, failures: h.failures, availability: h.availability, buckets };
}
function onError(status: number) {
  if (status === 404) return new APIError(404, "monitor_not_found", "Website not found. Reload the list to continue.");
  if (status === 400) return new APIError(400, "invalid_statistics", "Invalid monitoring request.");
}

/** Reads owner-scoped statistics through the shared BFF; POST permits secure cookie rotation. */
export function handleStatistics(req: NextRequest, action: "status" | "history", id?: string) {
  return handleAuthenticated(req, async () => {
    if (action === "status") {
      const ids = (req.nextUrl.searchParams.get("ids") ?? "").split(",");
      if (ids.length > 50 || ids.some(value => !uuid.test(value)) || new Set(ids.map(value => value.toLowerCase())).size !== ids.length) throw new APIError(400, "invalid_ids", "Provide 1 to 50 unique website IDs.");
      return { path: `monitors/status?ids=${encodeURIComponent(ids.join(","))}`, method: "GET", onError, decode: data => {
        const result = data as { monitors?: unknown[] } | null;
        if (!result || !Array.isArray(result.monitors) || result.monitors.length !== ids.length) throw invalid();
        const monitors = result.monitors.map(decodeCheck);
        const expected = new Set(ids.map(value => value.toLowerCase()));
        for (const monitor of monitors) { if (!expected.delete(monitor.id.toLowerCase())) throw invalid(); }
        return { monitors };
      } };
    }
    const period = req.nextUrl.searchParams.get("period") ?? "24h";
    if (!id || !uuid.test(id) || !["1h", "24h", "7d", "30d"].includes(period)) throw new APIError(400, "invalid_history", "Invalid website ID or history period.");
    return { path: `monitors/${id}/history?period=${period}`, method: "GET", onError, decode: decodeHistory };
  });
}
