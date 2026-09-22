export type Monitor = { id: string; url: string; interval_seconds: number; created_at: string };
export type CreateMonitor = Pick<Monitor, "url" | "interval_seconds">;
export const MAX_INTERVAL_SECONDS = 2_147_483_647;

export function validMonitorURL(value: string): boolean {
  if (new TextEncoder().encode(value).length > 2048 || /[\s\p{Cc}\\#]/u.test(value) || !/^https?:\/\//i.test(value)) return false;
  try {
    const url = new URL(value);
    return !!url.hostname && !url.username && !url.password;
  } catch { return false; }
}

export function formatInterval(seconds: number): string {
  const [value, unit] = seconds % 3600 === 0 ? [seconds / 3600, "hour"] : seconds % 60 === 0 ? [seconds / 60, "minute"] : [seconds, "second"];
  return `Every ${value} ${unit}${value === 1 ? "" : "s"}`;
}
