export function formatInterval(seconds: number): string {
  const [value, unit] = seconds % 3600 === 0 ? [seconds / 3600, "hour"] : seconds % 60 === 0 ? [seconds / 60, "minute"] : [seconds, "second"];
  return `Every ${value} ${unit}${value === 1 ? "" : "s"}`;
}
