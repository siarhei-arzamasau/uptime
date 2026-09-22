"use client";

import { useEffect, useId, useRef, useState } from "react";
import Link from "next/link";
import Image from "next/image";
import type { User } from "./types";
import styles from "./user-menu.module.css";

export function UserMenu({ user, busy, onSignOut }: { user: User; busy: boolean; onSignOut: () => void }) {
  const [open, setOpen] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panelId = useId();

  useEffect(() => {
    if (!open) return;
    function outside(event: PointerEvent) {
      if (event.target instanceof Node && !container.current?.contains(event.target)) setOpen(false);
    }
    function escape(event: KeyboardEvent) {
      if (event.key === "Escape") { setOpen(false); trigger.current?.focus(); }
    }
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    return () => { document.removeEventListener("pointerdown", outside); document.removeEventListener("keydown", escape); };
  }, [open]);

  return <div className={styles.container} ref={container} onBlur={event => {
    if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
  }}>
    <button ref={trigger} className={styles.trigger} type="button" aria-label="Account menu" aria-expanded={open} aria-controls={panelId} onClick={() => setOpen(!open)}>
      <span className={styles.avatar}>{user.avatar_url ? <Image src={user.avatar_url} alt="Your avatar" width={34} height={34} unoptimized /> : <span aria-hidden="true">{(user.name || user.email).charAt(0).toUpperCase()}</span>}</span>
      <span>My account</span><span className={styles.chevron} aria-hidden="true">{open ? "▴" : "▾"}</span>
    </button>
    {open && <div id={panelId} className={styles.panel}>
      <div className={styles.identity}>
        {user.name && <strong className={styles.name}>{user.name}</strong>}
        <Link href="/profile" onClick={() => setOpen(false)}>Profile</Link>
        <strong>{user.email}</strong>
      </div>
    </div>}
    <button type="button" className={`${styles.action} ${styles.signOut}`} disabled={busy} onClick={onSignOut}>{busy ? "Signing out…" : "Sign out"}</button>
  </div>;
}
