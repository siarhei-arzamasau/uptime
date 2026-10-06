"use client";

import Image from "next/image";
import { useState } from "react";
import styles from "./monitors.module.css";

/** Displays a decorative favicon, falling back to a globe if decoding fails. */
export function SiteIcon({ favicon }: { favicon?: string }) {
  const [failed, setFailed] = useState(false);
  return <span className={styles.siteIcon} aria-hidden="true">
    {favicon && !failed ? <Image src={favicon} alt="" width={24} height={24} unoptimized onError={() => setFailed(true)} /> :
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <circle cx="12" cy="12" r="9" />
        <ellipse cx="12" cy="12" rx="4" ry="9" />
        <path d="M3 12h18M5 6.5h14M5 17.5h14" />
      </svg>}
  </span>;
}
