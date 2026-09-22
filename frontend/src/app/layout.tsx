import type { Metadata } from "next";
import { cookies } from "next/headers";
import Link from "next/link";
import { ThemeToggle } from "@/components/theme-toggle";
import "./globals.css";

export const metadata: Metadata = { title: "Uptime — Your workspace", description: "Sign in to your Uptime workspace." };
export default async function RootLayout({ children }: LayoutProps<"/">) {
  const preference = (await cookies()).get("uptime_theme")?.value;
  const theme = preference === "light" || preference === "dark" ? preference : "system";
  return <html lang="en" data-theme={theme}><body>
    <header><Link href="/" className="brand"><span className="brandMark" aria-hidden="true">▂▆▃</span> uptime<span style={{ fontWeight: 400, color: "var(--muted)", fontSize: 13 }}> / workspace</span></Link><div className="headerActions"><ThemeToggle initialTheme={theme} /><div id="account-menu-slot" /></div></header>
    {children}
    <footer>Uptime workspace · Built for a clearer view.</footer>
  </body></html>;
}
