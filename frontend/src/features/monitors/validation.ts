export const MAX_INTERVAL_SECONDS = 2_147_483_647;

// Keep the accepted authority syntax identical to the Go API. International
// domain names use their ASCII (punycode) form; Unicode paths remain supported.
export function validMonitorURL(value: string): boolean {
  if (new TextEncoder().encode(value).length > 2048 || /[\s\p{Cc}\\#]/u.test(value) || /%(?![0-9a-f]{2})/i.test(value)) return false;
  const match = /^https?:\/\/(\[[0-9a-f:.]+\]|[a-z0-9.-]+)(?::([0-9]+))?(?:[/?]|$)/i.exec(value);
  if (!match || (match[2] && Number(match[2]) > 65535)) return false;
  const host = match[1];
  if (!host.startsWith("[")) {
    const domain = host.endsWith(".") ? host.slice(0, -1) : host;
    if (domain.length > 253 || domain.split(".").some(label => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(label))) return false;
    // WHATWG treats a numeric final label as IPv4, including hex and octal.
    // Accept only the unambiguous dotted-decimal spelling on both sides.
    if (/^(?:[0-9]+|0x[0-9a-f]*)$/i.test(domain.split(".").at(-1)!)) {
      if (host !== domain || !/^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$/.test(host) || host.split(".").some(part => Number(part) > 255)) return false;
    }
  }
  // Use the platform parser only for IPv6. DNS names follow the explicit
  // grammar above rather than WHATWG's implicit IDNA/IPv4 transformations.
  if (host.startsWith("[")) {
    try { return !!new URL(value).hostname; } catch { return false; }
  }
  return true;
}
