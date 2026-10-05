"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { AuthError } from "../auth/transport";
import { loadMonitors } from "./client";
import { MonitorForm } from "./monitor-form";
import { DeleteMonitorConfirmation } from "./delete-monitor-confirmation";
import { MonitorList } from "./monitor-list";
import type { Monitor } from "./types";
import styles from "./monitors.module.css";

/**
 * Renders monitor management and cursor pagination with separate load/save error recovery.
 * A confirmed 401 redirects to login; successful mutations update the list after the server confirms them.
 */
export function Monitors() {
  const router = useRouter();
  const [monitors, setMonitors] = useState<Monitor[]>();
  const [cursor, setCursor] = useState<string>();
  const [loadError, setLoadError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<{ action: "edit" | "delete"; monitor: Monitor }>();
  const actionButton = useRef<HTMLButtonElement | null>(null);
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
    if (!open && !selected && restoreFocus.current) {
      const button = actionButton.current?.isConnected ? actionButton.current : addButton.current;
      button?.focus(); restoreFocus.current = false; actionButton.current = null;
    }
  }, [open, selected]);

  function closeForm() { restoreFocus.current = true; setOpen(false); setSelected(undefined); }
  function unauthorized() { setOpen(false); setSelected(undefined); setMonitors(undefined); router.replace("/login"); }

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
      <button ref={addButton} className={styles.primary} aria-expanded={open} aria-controls="monitor-form" disabled={!monitors || open || !!selected} onClick={() => { setSaved(""); setOpen(true); }}>Add website</button>
    </div>
    {loadError ? <div role="alert" className={styles.alert}>{loadError} <button className={styles.secondary} onClick={() => { setLoadError(""); setAttempt(value => value + 1); }}>Try again</button></div> : !monitors ? <p role="status">Loading websites…</p> : null}
    {saved && <p role="status" className={styles.success}>{saved}</p>}
    {open && <MonitorForm onCancel={closeForm} onUnauthorized={unauthorized} onSaved={monitor => {
      setMonitors(current => [monitor, ...(current ?? [])]);
      closeForm(); setSaved("Website added. Checks have not started yet.");
    }} />}
    {selected?.action === "edit" && <MonitorForm key={selected.monitor.id} monitor={selected.monitor} onCancel={closeForm} onUnauthorized={unauthorized} onSaved={monitor => {
      setMonitors(current => current?.map(existing => existing.id === monitor.id ? monitor : existing));
      closeForm(); setSaved("Website updated.");
    }} />}
    {selected?.action === "delete" && <DeleteMonitorConfirmation monitor={selected.monitor} onCancel={closeForm} onUnauthorized={unauthorized} onDeleted={() => {
      setMonitors(current => current?.filter(monitor => monitor.id !== selected.monitor.id));
      closeForm(); setSaved("Website deleted.");
    }} />}
    {monitors && (monitors.length ? <MonitorList
      monitors={monitors}
      disabled={open || !!selected || loadingMore}
      onEdit={(monitor, button) => {
        actionButton.current = button; setSaved(""); setSelected({ action: "edit", monitor });
      }}
      onDelete={(monitor, button) => {
        actionButton.current = button; setSaved(""); setSelected({ action: "delete", monitor });
      }}
    /> : !open && cursor ? <p className={styles.intro}>Load more to see the remaining websites.</p> : !open ? <div className={styles.empty}>
      <span className={styles.emptyMark} aria-hidden="true">—</span>
      <h2>No websites yet</h2><p>Add your first website to set up a monitor.</p>
    </div> : null)}
    {pageError && <p role="alert" className={styles.alert}>{pageError}</p>}
    {cursor && <button className={styles.secondary} disabled={loadingMore || !!selected} onClick={loadMore}>{loadingMore ? "Loading…" : "Load more"}</button>}
  </section>;
}
