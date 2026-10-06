import { authenticatedRequest } from "../auth/transport";
import type { CreateMonitor, Monitor, MonitorPage, MonitorCheck, MonitorHistory, HistoryPeriod } from "./types";

/**
 * Returns a monitor page after an optional exclusive cursor, refreshing cookies if needed.
 * Uses POST for the read because it may rotate cookies; failures reject as in authenticatedRequest.
 */
export function loadMonitors(cursor?: string) {
  return authenticatedRequest<MonitorPage>(`monitors${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`);
}
/** Returns { monitor } and schedules an immediate background check; failures reject as in authenticatedRequest. */
export function createMonitor(body: CreateMonitor) {
  return authenticatedRequest<{ monitor: Monitor }>("monitors/create", body);
}

/** Replaces URL/interval and returns the saved monitor; auth/service failures reject. */
export function updateMonitor(id: string, body: CreateMonitor) {
  return authenticatedRequest<{ monitor: Monitor }>(`monitors/${encodeURIComponent(id)}/update`, body);
}
/** Permanently deletes a monitor; auth/service failures reject. */
export function deleteMonitor(id: string) {
  return authenticatedRequest<{ ok: boolean }>(`monitors/${encodeURIComponent(id)}/delete`);
}

/** Returns fresh statuses without loading favicons; failures follow authenticatedRequest. */
export function loadStatuses(ids: string[]) {
  return authenticatedRequest<{ monitors: MonitorCheck[] }>(`monitors/status?ids=${encodeURIComponent(ids.join(","))}`);
}
/** Returns sample-weighted history for the selected window; failures follow authenticatedRequest. */
export function loadHistory(id: string, period: HistoryPeriod) {
  return authenticatedRequest<MonitorHistory>(`monitors/${encodeURIComponent(id)}/history?period=${period}`);
}
