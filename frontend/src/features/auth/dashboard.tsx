"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { AuthError, session, signOut, subscribeAuth } from "./client";
import type { User } from "./types";
import styles from "./auth.module.css";

export function Dashboard() {
  const router = useRouter();
  const [user, setUser] = useState<User>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const paused = useRef(false);
  const mounted = useRef(false);
  const generation = useRef(0);
  const check = useCallback(async () => {
    const current = ++generation.current;
    try {
      const result = await session();
      if (!mounted.current || current !== generation.current) return;
      setUser(result.user); setError(""); paused.current = false;
    } catch (e) {
      if (!mounted.current || current !== generation.current) return;
      setUser(undefined);
      if (e instanceof AuthError && e.status === 401) { router.replace("/login"); return; }
      paused.current = true; setError(e instanceof Error ? e.message : "Unable to load your workspace.");
    }
  }, [router]);
  useEffect(() => {
    mounted.current = true;
    let active = true;
    queueMicrotask(() => { if (active) void check(); });
    const focus = () => { if (!paused.current && document.visibilityState === "visible") void check(); };
    window.addEventListener("focus", focus);
    document.addEventListener("visibilitychange", focus);
    const unsubscribe = subscribeAuth(() => { setUser(undefined); paused.current = false; void check(); });
    return () => { active = false; mounted.current = false; unsubscribe(); window.removeEventListener("focus", focus); document.removeEventListener("visibilitychange", focus); };
  }, [check]);
  async function logout() {
    if (busy) return;
    setBusy(true); paused.current = true; generation.current++;
    try { await signOut(); setUser(undefined); router.replace("/login"); router.refresh(); }
    catch (e) { setError(e instanceof Error ? e.message : "Unable to sign out. Please try again."); }
    finally { setBusy(false); }
  }
  return <main className={styles.dashboard}>
    <div className={styles.dashboardHead}><div><p className={styles.eyebrow}>Your workspace</p><h1 className={styles.title}>Good to have you here.</h1></div>{user && <button className={styles.secondary} onClick={logout} disabled={busy}>{busy ? "Signing out…" : "Sign out"}</button>}</div>
    {error && <div role="alert" className={styles.alert}>{error} {!user && <button className={styles.secondary} onClick={() => void check()}>Try again</button>}</div>}
    {!user && !error && <p role="status">Loading your workspace…</p>}
    {user && <section className={styles.account}><h2>Your account</h2><dl><dt>Email address</dt><dd>{user.email}</dd></dl><p className={styles.empty}>You’re signed in. Your workspace is ready for what comes next.</p></section>}
  </main>;
}
