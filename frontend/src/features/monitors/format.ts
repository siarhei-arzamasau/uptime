/**
 * Formats validated whole seconds using the largest exact hour/minute unit.
 * It does not validate the input; creation policy is enforced before calling it.
 */
export function formatInterval(seconds: number): string {
  // Use larger units only when exact so custom intervals do not lose precision.
  const [value, unit] = seconds % 3600 === 0 ? [seconds / 3600, "hour"] : seconds % 60 === 0 ? [seconds / 60, "minute"] : [seconds, "second"];
  return `Every ${value} ${unit}${value === 1 ? "" : "s"}`;
}
