import { useEffect, useRef, useState, type FormEvent } from "react";
import { AuthError } from "../auth/transport";
import { createMonitor } from "./client";
import { MAX_INTERVAL_SECONDS, validMonitorURL } from "./validation";
import type { Monitor } from "./types";
import styles from "./monitors.module.css";

const units = [{ label: "Seconds", seconds: 1 }, { label: "Minutes", seconds: 60 }, { label: "Hours", seconds: 3600 }];

export function CreateMonitorForm({ onCreated, onCancel, onUnauthorized }: {
  onCreated: (monitor: Monitor) => void;
  onCancel: () => void;
  onUnauthorized: () => void;
}) {
  const [url, setURL] = useState("");
  const [interval, setInterval] = useState("1");
  const [unit, setUnit] = useState(60);
  const [fieldError, setFieldError] = useState<{ field: "url" | "interval"; message: string }>();
  const [saveError, setSaveError] = useState("");
  const [busy, setBusy] = useState(false);
  const saving = useRef(false);
  const urlInput = useRef<HTMLInputElement>(null);
  const intervalInput = useRef<HTMLInputElement>(null);

  useEffect(() => { urlInput.current?.focus(); }, []);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving.current) return;
    setFieldError(undefined); setSaveError("");
    const trimmedURL = url.trim();
    if (!validMonitorURL(trimmedURL)) {
      setFieldError({ field: "url", message: "Enter an HTTP or HTTPS URL up to 2048 bytes without credentials or a fragment; use punycode for international domains." });
      urlInput.current?.focus(); return;
    }
    const count = Number(interval), seconds = count * unit;
    if (!Number.isInteger(count) || count < 1 || seconds > MAX_INTERVAL_SECONDS) {
      setFieldError({ field: "interval", message: `Enter a whole number from 1 to ${Math.floor(MAX_INTERVAL_SECONDS / unit).toLocaleString("en-US")}.` });
      intervalInput.current?.focus(); return;
    }
    saving.current = true; setBusy(true);
    try {
      const result = await createMonitor({ url: trimmedURL, interval_seconds: seconds });
      onCreated(result.monitor);
    } catch (error) {
      if (error instanceof AuthError && error.status === 401) { onUnauthorized(); }
      else setSaveError(error instanceof Error ? error.message : "Unable to create the monitor. Please try again.");
    } finally { saving.current = false; setBusy(false); }
  }

  return <form id="create-monitor" aria-labelledby="create-monitor-title" className={styles.form} onSubmit={submit} noValidate>
    <h2 id="create-monitor-title">Add a website</h2>
    <div className={styles.fields}>
      <div className={styles.urlField}>
        <label htmlFor="monitor-url">Website URL</label>
        <input ref={urlInput} id="monitor-url" type="url" required placeholder="https://example.com" value={url} disabled={busy} onChange={event => setURL(event.target.value)} aria-invalid={fieldError?.field === "url"} aria-describedby={fieldError?.field === "url" ? "monitor-url-error" : undefined} />
        {fieldError?.field === "url" && <p id="monitor-url-error" className={styles.error}>{fieldError.message}</p>}
      </div>
      <fieldset className={styles.intervalField}>
        <legend>Check every</legend>
        <div className={styles.intervalInputs}>
          <input ref={intervalInput} aria-label="Check interval" type="number" min="1" max={Math.floor(MAX_INTERVAL_SECONDS / unit)} step="1" required value={interval} disabled={busy} onChange={event => setInterval(event.target.value)} aria-invalid={fieldError?.field === "interval"} aria-describedby={fieldError?.field === "interval" ? "monitor-interval-error" : undefined} />
          <select aria-label="Interval unit" value={unit} disabled={busy} onChange={event => setUnit(Number(event.target.value))}>{units.map(option => <option key={option.seconds} value={option.seconds}>{option.label}</option>)}</select>
        </div>
        {fieldError?.field === "interval" && <p id="monitor-interval-error" className={styles.error}>{fieldError.message}</p>}
      </fieldset>
    </div>
    <p className={styles.note}>Your settings will be saved. Monitoring checks are not running yet.</p>
    {saveError && <p role="alert" className={styles.alert}>{saveError}</p>}
    <div className={styles.actions}><button type="button" className={styles.secondary} disabled={busy} onClick={onCancel}>Cancel</button><button className={styles.primary} disabled={busy}>{busy ? "Creating…" : "Create"}</button></div>
  </form>;
}
