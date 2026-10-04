"use client";
import { useState } from "react";

/**
 * Renders a theme switch using the initial SSR preference; user changes update the DOM
 * and a one-year preference cookie. A system preference resolves at the time of toggling.
 */
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
