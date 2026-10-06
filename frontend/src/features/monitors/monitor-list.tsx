import { useState } from "react";
import { SiteIcon } from "./site-icon";
import { HistoryChart } from "./history-chart";
import type { Monitor, MonitorCheck } from "./types";
import { formatInterval } from "./format";
import styles from "./monitors.module.css";

const reasons: Record<string, string> = {
  timeout: "Request timed out", dns: "DNS lookup failed", tls: "TLS verification failed",
  connection: "Connection failed", blocked_address: "Address is not public", invalid_url: "Invalid URL",
};
function statusText(check: MonitorCheck | undefined, stale: boolean) {
  if (!check || check.last_success === null) return "Awaiting first check";
  const last = check.last_success ? "Working" : "Unavailable";
  return stale || check.status === "stale" ? `No fresh data · Last result: ${last.toLowerCase()}` : last;
}

/** Renders current observations and one expandable history chart while preserving CRUD controls. */
export function MonitorList({ monitors, disabled, now, onEdit, onDelete }: {
  monitors: Monitor[];
  disabled: boolean;
  now: number;
  onEdit: (monitor: Monitor, button: HTMLButtonElement) => void;
  onDelete: (monitor: Monitor, button: HTMLButtonElement) => void;
}) {
  const [expanded, setExpanded] = useState<string>();
  return <div className={styles.list}>
    <div className={styles.listHeading}><h2>Websites <span>{monitors.length}</span></h2><p>Live checks · HTTP 200 only</p></div>
    <ul>{monitors.map(monitor => {
      const check = monitor.check;
      const stale = !!check?.last_started_at && now > Date.parse(check.last_started_at) + (monitor.interval_seconds + 15) * 1000;
      const state = stale ? "stale" : check?.status ?? "pending";
      return <li key={monitor.id} className={styles.monitorItem}>
        <div className={styles.row}>
          <div className={styles.website}><SiteIcon key={`${monitor.url}:${monitor.favicon ?? ""}`} favicon={monitor.favicon} /><div>
            <p className={styles.url}>{monitor.url}</p>
            <span className={`${styles.status} ${styles[state]}`}>{statusText(check, stale)}</span>
            {check?.last_started_at && <span className={styles.lastCheck}>Last checked <time dateTime={check.last_started_at}>{new Date(check.last_started_at).toLocaleString()}</time>{check.http_status ? ` · HTTP ${check.http_status}` : ""}{check.error_kind && check.error_kind !== "http_status" ? ` · ${reasons[check.error_kind] ?? "Check failed"}` : ""}</span>}
          </div></div>
          <div className={styles.rowControls}>
            <p className={styles.frequency}>{formatInterval(monitor.interval_seconds)}</p>
            <div className={styles.actions}>
              <button className={styles.secondary} aria-label={`History for ${monitor.url}`} aria-expanded={expanded === monitor.id} aria-controls={`history-${monitor.id}`} onClick={() => setExpanded(current => current === monitor.id ? undefined : monitor.id)}>History</button>
              <button className={styles.secondary} disabled={disabled} aria-label={`Edit ${monitor.url}`} onClick={event => onEdit(monitor, event.currentTarget)}>Edit</button>
              <button className={styles.danger} disabled={disabled} aria-label={`Delete ${monitor.url}`} onClick={event => onDelete(monitor, event.currentTarget)}>Delete</button>
            </div>
          </div>
        </div>
        {expanded === monitor.id && <HistoryChart key={`${monitor.id}:${monitor.url}:${check?.version ?? 0}`} id={monitor.id} version={check?.version} />}
      </li>;
    })}</ul>
  </div>;
}
