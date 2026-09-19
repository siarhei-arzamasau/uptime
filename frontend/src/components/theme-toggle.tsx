"use client";
import { useState } from "react";

export function ThemeToggle({ initialTheme }: { initialTheme: string }) {
  const [theme, setTheme] = useState(initialTheme);
  function toggle() {
    const dark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    const next = dark ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    document.cookie = `uptime_theme=${next}; Path=/; Max-Age=31536000; SameSite=Lax${location.protocol === "https:" ? "; Secure" : ""}`;
    setTheme(next);
  }
  return <button className="themeToggle" type="button" onClick={toggle} aria-label="Toggle color theme"><span aria-hidden="true">◐</span> Theme</button>;
}
