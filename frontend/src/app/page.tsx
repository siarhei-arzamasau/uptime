import { redirect } from "next/navigation";
/** Redirects the root route to the dashboard using Next.js redirect control flow. */
export default function Home() { redirect("/dashboard"); }
