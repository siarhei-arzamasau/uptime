import { SiteIcon } from "./site-icon";
import type { Monitor } from "./types";
import { formatInterval } from "./format";
import styles from "./monitors.module.css";

/** Renders URLs as plain text with their configured intervals; no monitoring result is implied. */
export function MonitorList({ monitors, disabled, onEdit, onDelete }: {
  monitors: Monitor[];
  disabled: boolean;
  onEdit: (monitor: Monitor, button: HTMLButtonElement) => void;
  onDelete: (monitor: Monitor, button: HTMLButtonElement) => void;
}) {
  return <div className={styles.list}>
    <div className={styles.listHeading}><h2>Websites <span>{monitors.length}</span></h2><p>Checks have not started yet</p></div>
    <ul>{monitors.map(monitor => <li key={monitor.id} className={styles.row}>
      <div className={styles.website}><SiteIcon key={`${monitor.url}:${monitor.favicon ?? ""}`} favicon={monitor.favicon} /><div><p className={styles.url}>{monitor.url}</p><span className={styles.status}>Not checked yet</span></div></div>
      <div className={styles.rowControls}>
        <p className={styles.frequency}>{formatInterval(monitor.interval_seconds)}</p>
        <div className={styles.actions}>
          <button className={styles.secondary} disabled={disabled} aria-label={`Edit ${monitor.url}`} onClick={event => onEdit(monitor, event.currentTarget)}>Edit</button>
          <button className={styles.danger} disabled={disabled} aria-label={`Delete ${monitor.url}`} onClick={event => onDelete(monitor, event.currentTarget)}>Delete</button>
        </div>
      </div>
    </li>)}</ul>
  </div>;
}
