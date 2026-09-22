import { authenticatedRequest } from "../auth/transport";
import type { CreateMonitor, Monitor, MonitorPage } from "./types";

export function loadMonitors(cursor?: string) {
  return authenticatedRequest<MonitorPage>(`monitors${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`);
}
export function createMonitor(body: CreateMonitor) {
  return authenticatedRequest<{ monitor: Monitor }>("monitors/create", body);
}
