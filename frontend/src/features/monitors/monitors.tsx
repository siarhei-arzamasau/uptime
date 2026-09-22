"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { AuthError, createMonitor, loadMonitors } from "../auth/client";
import { formatInterval, MAX_INTERVAL_SECONDS, validMonitorURL, type Monitor } from "./types";
import styles from "./monitors.module.css";

const units = [{ label: "Seconds", seconds: 1 }, { label: "Minutes", seconds: 60 }, { label: "Hours", seconds: 3600 }];

export function Monitors() {
  const router = useRouter();
  const [monitors, setMonitors] = useState<Monitor[]>();
  const [loadError, setLoadError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [open, setOpen] = useState(false);
  const [url, setURL] = useState("");
  const [interval, setInterval] = useState("1");
  const [unit, setUnit] = useState(60);
  const [fieldError, setFieldError] = useState<{ field: "url" | "interval"; message: string }>();
  const [saveError, setSaveError] = useState("");
  const [saved, setSaved] = useState("");
  const [busy, setBusy] = useState(false);
  const saving = useRef(false);
  const addButton = useRef<HTMLButtonElement>(null);
  const urlInput = useRef<HTMLInputElement>(null);
  const intervalInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let active = true;
    void loadMonitors().then(result => {
      if (active) setMonitors(result.monitors);
    }).catch(error => {
      if (!active) return;
      if (error instanceof AuthError && error.status === 401) router.replace("/login");
      else setLoadError(error instanceof Error ? error.message : "Unable to load websites. Please try again.");
    });
    return () => { active = false; };
  }, [attempt, router]);

  useEffect(() => { if (open) urlInput.current?.focus(); else addButton.current?.focus(); }, [open]);

  function closeForm() {
    setOpen(false); setURL(""); setInterval("1"); setUnit(60); setFieldError(undefined); setSaveError("");
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving.current) return;
    setFieldError(undefined); setSaveError(""); setSaved("");
    const trimmedURL = url.trim();
    if (!validMonitorURL(trimmedURL)) {
      setFieldError({ field: "url", message: "Enter an HTTP or HTTPS URL up to 2048 bytes without credentials or a fragment." });
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
      setMonitors(current => [result.monitor, ...(current ?? [])]);
      closeForm(); setSaved("Website added. Checks have not started yet.");
    } catch (error) {
      if (error instanceof AuthError && error.status === 401) { setMonitors(undefined); router.replace("/login"); }
      else setSaveError(error instanceof Error ? error.message : "Unable to create the monitor. Please try again.");
    } finally { saving.current = false; setBusy(false); }
  }

  return <section aria-label="Website monitors" className={styles.monitors}>
    <div className={styles.toolbar}>
      <p className={styles.intro}>Save your websites and choose how often to check them.</p>
      <button ref={addButton} className={styles.primary} aria-expanded={open} aria-controls="create-monitor" disabled={!monitors || busy} onClick={() => { setSaved(""); setOpen(true); }}>Add website</button>
    </div>
    {loadError ? <div role="alert" className={styles.alert}>{loadError} <button className={styles.secondary} onClick={() => { setLoadError(""); setAttempt(value => value + 1); }}>Try again</button></div> : !monitors ? <p role="status">Loading websites…</p> : null}
    {saved && <p role="status" className={styles.success}>{saved}</p>}
    {open && <form id="create-monitor" aria-labelledby="create-monitor-title" className={styles.form} onSubmit={submit} noValidate>
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
      <div className={styles.actions}><button type="button" className={styles.secondary} disabled={busy} onClick={closeForm}>Cancel</button><button className={styles.primary} disabled={busy}>{busy ? "Creating…" : "Create"}</button></div>
    </form>}
    {monitors && (monitors.length ? <div className={styles.list}>
      <div className={styles.listHeading}><h2>Websites <span>{monitors.length}</span></h2><p>Checks have not started yet</p></div>
      <ul>{monitors.map(monitor => <li key={monitor.id} className={styles.row}>
        <div className={styles.website}><span className={styles.marker} aria-hidden="true" /><div><p className={styles.url}>{monitor.url}</p><span className={styles.status}>Not checked yet</span></div></div>
        <p className={styles.frequency}>{formatInterval(monitor.interval_seconds)}</p>
      </li>)}</ul>
    </div> : !open ? <div className={styles.empty}><span className={styles.emptyMark} aria-hidden="true">—</span><h2>No websites yet</h2><p>Add your first website to set up a monitor.</p></div> : null)}
  </section>;
}
