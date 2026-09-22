"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { AuthError } from "../auth/transport";
import { loadMonitors } from "./client";
import { CreateMonitorForm } from "./create-monitor-form";
import { MonitorList } from "./monitor-list";
import type { Monitor } from "./types";
import styles from "./monitors.module.css";

export function Monitors() {
  const router = useRouter();
  const [monitors, setMonitors] = useState<Monitor[]>();
  const [cursor, setCursor] = useState<string>();
  const [loadError, setLoadError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [pageError, setPageError] = useState("");
  const paging = useRef(false);
  const mounted = useRef(false);
  const addButton = useRef<HTMLButtonElement>(null);
  const restoreFocus = useRef(false);

  useEffect(() => {
    let active = true;
    mounted.current = true;
    void loadMonitors().then(result => {
      if (active) { setMonitors(result.monitors); setCursor(result.next_cursor); }
    }).catch(error => {
      if (!active) return;
      if (error instanceof AuthError && error.status === 401) router.replace("/login");
      else setLoadError(error instanceof Error ? error.message : "Unable to load websites. Please try again.");
    });
    return () => { active = false; mounted.current = false; };
  }, [attempt, router]);

  useEffect(() => {
    if (!open && restoreFocus.current) { addButton.current?.focus(); restoreFocus.current = false; }
  }, [open]);

  function closeForm() { restoreFocus.current = true; setOpen(false); }
  function unauthorized() { setMonitors(undefined); router.replace("/login"); }

  async function loadMore() {
    if (!cursor || paging.current) return;
    paging.current = true;
    setLoadingMore(true); setPageError("");
    try {
      const result = await loadMonitors(cursor);
      if (!mounted.current) return;
      setMonitors(current => {
        const ids = new Set(current?.map(monitor => monitor.id));
        return [...(current ?? []), ...result.monitors.filter(monitor => !ids.has(monitor.id))];
      });
      setCursor(result.next_cursor);
    } catch (error) {
      if (!mounted.current) return;
      if (error instanceof AuthError && error.status === 401) unauthorized();
      else setPageError(error instanceof Error ? error.message : "Unable to load more websites.");
    } finally {
      paging.current = false;
      if (mounted.current) setLoadingMore(false);
    }
  }

  return <section aria-label="Website monitors" className={styles.monitors}>
    <div className={styles.toolbar}>
      <p className={styles.intro}>Save your websites and choose how often to check them.</p>
      <button ref={addButton} className={styles.primary} aria-expanded={open} aria-controls="create-monitor" disabled={!monitors || open} onClick={() => { setSaved(""); setOpen(true); }}>Add website</button>
    </div>
    {loadError ? <div role="alert" className={styles.alert}>{loadError} <button className={styles.secondary} onClick={() => { setLoadError(""); setAttempt(value => value + 1); }}>Try again</button></div> : !monitors ? <p role="status">Loading websites…</p> : null}
    {saved && <p role="status" className={styles.success}>{saved}</p>}
    {open && <CreateMonitorForm onCancel={closeForm} onUnauthorized={unauthorized} onCreated={monitor => {
      setMonitors(current => [monitor, ...(current ?? [])]);
      closeForm(); setSaved("Website added. Checks have not started yet.");
    }} />}
    {monitors && (monitors.length ? <MonitorList monitors={monitors} /> : !open ? <div className={styles.empty}><span className={styles.emptyMark} aria-hidden="true">—</span><h2>No websites yet</h2><p>Add your first website to set up a monitor.</p></div> : null)}
    {pageError && <p role="alert" className={styles.alert}>{pageError}</p>}
    {cursor && <button className={styles.secondary} disabled={loadingMore} onClick={loadMore}>{loadingMore ? "Loading…" : "Load more"}</button>}
  </section>;
}
