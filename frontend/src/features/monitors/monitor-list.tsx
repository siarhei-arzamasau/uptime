import type { Monitor } from "./types";
import { formatInterval } from "./format";
import styles from "./monitors.module.css";

export function MonitorList({ monitors }: { monitors: Monitor[] }) {
  return <div className={styles.list}>
    <div className={styles.listHeading}><h2>Websites <span>{monitors.length}</span></h2><p>Checks have not started yet</p></div>
    <ul>{monitors.map(monitor => <li key={monitor.id} className={styles.row}>
      <div className={styles.website}><span className={styles.marker} aria-hidden="true" /><div><p className={styles.url}>{monitor.url}</p><span className={styles.status}>Not checked yet</span></div></div>
      <p className={styles.frequency}>{formatInterval(monitor.interval_seconds)}</p>
    </li>)}</ul>
  </div>;
}
