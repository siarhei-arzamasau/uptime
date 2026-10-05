import { useEffect, useRef, useState } from "react";
import { AuthError } from "../auth/transport";
import { deleteMonitor } from "./client";
import type { Monitor } from "./types";
import styles from "./monitors.module.css";

/** Confirms permanent deletion; failures preserve the confirmation and a 401 ends the session. */
export function DeleteMonitorConfirmation({ monitor, onCancel, onDeleted, onUnauthorized }: {
  monitor: Monitor;
  onCancel: () => void;
  onDeleted: () => void;
  onUnauthorized: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const deleting = useRef(false);
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => { cancel.current?.focus(); }, []);

  async function confirm() {
    if (deleting.current) return;
    deleting.current = true; setBusy(true); setError("");
    try {
      await deleteMonitor(monitor.id);
      onDeleted();
    } catch (error) {
      if (error instanceof AuthError && error.status === 401) onUnauthorized();
      else setError(error instanceof Error ? error.message : "Unable to delete the website. Please try again.");
    } finally { deleting.current = false; setBusy(false); }
  }

  return <section aria-labelledby="delete-monitor-title" className={styles.form}>
    <h2 id="delete-monitor-title">Delete website?</h2>
    <p className={styles.url}>{monitor.url}</p>
    <p className={styles.note}>This will permanently remove the website and its check interval. This action cannot be undone.</p>
    {error && <p role="alert" className={styles.alert}>{error}</p>}
    <div className={styles.actions}>
      <button ref={cancel} className={styles.secondary} disabled={busy} onClick={onCancel}>Cancel</button>
      <button className={styles.danger} disabled={busy} onClick={confirm}>{busy ? "Deleting…" : "Delete website"}</button>
    </div>
  </section>;
}
