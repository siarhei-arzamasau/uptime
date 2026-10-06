export type MonitorCheck = {
  id: string;
  version: number;
  status: "pending" | "up" | "down" | "stale";
  last_started_at: string | null;
  last_finished_at: string | null;
  last_success: boolean | null;
  http_status: number | null;
  error_kind: string;
};
export type Monitor = { id: string; url: string; interval_seconds: number; created_at: string; favicon?: string; check?: MonitorCheck };
export type CreateMonitor = Pick<Monitor, "url" | "interval_seconds">;
export type MonitorPage = { monitors: Monitor[]; next_cursor?: string };
export type HistoryPeriod = "1h" | "24h" | "7d" | "30d";
export type HistoryBucket = { start: string; end: string; successes: number; failures: number; availability: number | null };
export type MonitorHistory = {
  from: string; to: string; step_seconds: number; version: number;
  successes: number; failures: number; availability: number | null; buckets: HistoryBucket[];
};
