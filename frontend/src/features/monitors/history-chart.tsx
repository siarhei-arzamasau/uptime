"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { AuthError } from "../auth/transport";
import { loadHistory } from "./client";
import type { HistoryPeriod, MonitorHistory } from "./types";
import styles from "./monitors.module.css";

const periods: { value: HistoryPeriod; label: string }[] = [
  { value: "1h", label: "1 hour" }, { value: "24h", label: "24 hours" },
  { value: "7d", label: "7 days" }, { value: "30d", label: "30 days" },
];
const percent = (value: number | null) => value === null ? "No data" : `${value.toFixed(2)}%`;

/** Shows sample availability with explicit gaps and keyboard/touch-accessible bucket details. */
export function HistoryChart({ id, version }: { id: string; version?: number }) {
  const router = useRouter();
  const [period, setPeriod] = useState<HistoryPeriod>("24h");
  const [result, setResult] = useState<{ period: HistoryPeriod; history: MonitorHistory }>();
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<number>();
  const busy = useRef(false);
  const history = result?.period === period ? result.history : undefined;

  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function refresh() {
      if (!active) return;
      if (document.hidden || busy.current) { timer = setTimeout(refresh, 1000); return; }
      busy.current = true;
      try {
        const data = await loadHistory(id, period);
        if (active && (version === undefined || data.version >= version)) {
          setResult({ period, history: data }); setError("");
        }
      } catch (err) {
        if (active) {
          if (err instanceof AuthError && err.status === 401) router.replace("/login");
          else setError(err instanceof Error ? err.message : "Unable to load history.");
        }
      } finally {
        busy.current = false;
        if (active) { clearTimeout(timer); timer = setTimeout(refresh, 30_000); }
      }
    }
    function visible() { if (!document.hidden) { clearTimeout(timer); void refresh(); } }
    void refresh();
    document.addEventListener("visibilitychange", visible);
    return () => { active = false; clearTimeout(timer); document.removeEventListener("visibilitychange", visible); };
  }, [id, period, version, router]);

  const index = history ? Math.min(selected ?? history.buckets.length - 1, history.buckets.length - 1) : -1;
  const bucket = history?.buckets[index];
  return <section id={`history-${id}`} className={styles.history} aria-label="Availability history">
    <div className={styles.historyHeading}>
      <div><h3>Availability</h3><strong>{history ? percent(history.availability) : "—"}</strong></div>
      <div className={styles.periods} aria-label="History period">{periods.map(item => <button type="button" key={item.value} aria-pressed={period === item.value} onClick={() => { setPeriod(item.value); setSelected(undefined); setError(""); }}>{item.label}</button>)}</div>
    </div>
    {error && <p role="alert" className={styles.alert}>{error} {history ? "Showing the last loaded history." : ""}</p>}
    {!history ? (!error && <p role="status">Loading history…</p>) : <>
      <div className={styles.chartWrap}>
        <span className={styles.axisTop}>100%</span><span className={styles.axisBottom}>0%</span>
        <svg viewBox="0 0 720 150" role="img" aria-label="Percentage of successful checks by time interval. Empty intervals have no data." preserveAspectRatio="none" className={styles.chart}>
          <line x1="0" y1="5" x2="720" y2="5" className={styles.gridLine} />
          <line x1="0" y1="140" x2="720" y2="140" className={styles.gridLine} />
          {history.buckets.map((point, i) => {
            const width = 720 / history.buckets.length;
            const height = point.availability === null ? 0 : Math.max(2, point.availability * 1.35);
            return <g key={point.start}>
              {point.availability !== null && <rect x={i * width} y={140 - height} width={Math.max(1, width - 1)} height={height} className={point.failures > 0 ? styles.failedBar : styles.goodBar} opacity={index === i ? 1 : 0.7} />}
              <rect x={i * width} y="0" width={width} height="150" fill="transparent" onPointerDown={() => setSelected(i)}><title>{new Date(point.start).toLocaleString()}: {percent(point.availability)}</title></rect>
            </g>;
          })}
        </svg>
      </div>
      <div className={styles.chartDates}><span>{new Date(history.from).toLocaleString()}</span><span>{new Date(history.to).toLocaleString()}</span></div>
      {bucket && <>
        <label className={styles.bucketLabel} htmlFor={`bucket-${id}`}>Inspect time interval</label>
        <input id={`bucket-${id}`} className={styles.bucketSlider} type="range" min="0" max={history.buckets.length - 1} value={index} onChange={event => setSelected(Number(event.target.value))} aria-valuetext={`${new Date(bucket.start).toLocaleString()}: ${percent(bucket.availability)}`} />
        <p className={styles.bucketDetail} aria-live="polite">{new Date(bucket.start).toLocaleString()} – {new Date(bucket.end).toLocaleTimeString()}: <strong>{percent(bucket.availability)}</strong> · {bucket.successes} successful / {bucket.failures} failed</p>
      </>}
      {history.availability === null && <p className={styles.note}>No checks in this period yet. Gaps mean no data, not downtime.</p>}
      <p className={styles.note}>{history.successes + history.failures} checks · Percentage of successful samples, not exact elapsed uptime. History is kept for 30 days.</p>
    </>}
  </section>;
}
