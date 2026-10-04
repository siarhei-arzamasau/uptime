import { authenticatedRequest } from "../auth/transport";
import type { CreateMonitor, Monitor, MonitorPage } from "./types";

/**
 * Returns a monitor page after an optional exclusive cursor, refreshing cookies if needed.
 * Uses POST for the read because it may rotate cookies; failures reject as in authenticatedRequest.
 */
export function loadMonitors(cursor?: string) {
  return authenticatedRequest<MonitorPage>(`monitors${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`);
}
/** Returns { monitor } without starting checks; failures reject as in authenticatedRequest. */
export function createMonitor(body: CreateMonitor) {
  return authenticatedRequest<{ monitor: Monitor }>("monitors/create", body);
}
