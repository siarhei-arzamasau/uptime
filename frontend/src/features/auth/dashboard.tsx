"use client";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useRouter } from "next/navigation";
import { AuthError, session, signOut, subscribeAuth } from "./client";
import type { AuthResult, User } from "./types";
import { UserMenu } from "./user-menu";
import { Monitors } from "../monitors/monitors";
import styles from "./auth.module.css";

export function Workspace({ title, load = session, children }: {
  title: string;
  load?: () => Promise<AuthResult>;
  children?: (user: User, update: (user: User) => void) => ReactNode;
}) {
  const router = useRouter();
  const [user, setUser] = useState<User>();
  const [error, setError] = useState<{ message: string; retry: boolean }>();
  const [busy, setBusy] = useState(false);
  const paused = useRef(false);
  const mounted = useRef(false);
  const generation = useRef(0);
  const check = useCallback(async () => {
    const current = ++generation.current;
    try {
      const result = await load();
      if (!mounted.current || current !== generation.current) return;
      setUser(result.user); setError(undefined); paused.current = false;
    } catch (e) {
      if (!mounted.current || current !== generation.current) return;
      if (e instanceof AuthError && e.status === 401) { setUser(undefined); router.replace("/login"); return; }
      paused.current = true; setError({ message: e instanceof Error ? e.message : "Unable to load your workspace.", retry: true });
    }
  }, [router, load]);
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
    catch (e) { setError({ message: e instanceof Error ? e.message : "Unable to sign out. Please try again.", retry: false }); }
    finally { setBusy(false); }
  }
  const accountSlot = typeof document !== "undefined" ? document.getElementById("account-menu-slot") : null;
  return <main className={styles.dashboard}>
    {user && accountSlot && createPortal(<UserMenu user={user} busy={busy} onSignOut={logout} />, accountSlot)}
    <div className={styles.dashboardHead}><div><p className={styles.eyebrow}>Your workspace</p><h1 className={styles.title}>{title}</h1></div></div>
    {error && <div role="alert" className={styles.alert}>{error.message} {error.retry && <button className={styles.secondary} onClick={() => void check()}>Try again</button>}</div>}
    {!user && !error && <p role="status">Loading your workspace…</p>}
    {user && children?.(user, setUser)}
  </main>;
}

export function Dashboard() { return <Workspace title="Your websites">{user => <Monitors key={user.id} />}</Workspace>; }
