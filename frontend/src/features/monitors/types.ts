export type Monitor = { id: string; url: string; interval_seconds: number; created_at: string };
export type CreateMonitor = Pick<Monitor, "url" | "interval_seconds">;
export type MonitorPage = { monitors: Monitor[]; next_cursor?: string };
