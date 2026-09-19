"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { signIn } from "./client";
import styles from "./auth.module.css";

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const register = mode === "register";
  const router = useRouter();
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const sending = useRef(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [failure, setFailure] = useState("");
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (sending.current) return;
    const form = event.currentTarget;
    const values = new FormData(form);
    const email = String(values.get("email") ?? "").trim();
    const password = String(values.get("password") ?? "");
    const next: Record<string, string> = {};
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) next.email = "Enter a valid email address.";
    if (Array.from(password).length < 12 || Array.from(password).length > 128) next.password = "Use between 12 and 128 characters.";
    if (register && values.get("confirm") !== password) next.confirm = "Passwords do not match.";
    setErrors(next); setFailure("");
    if (Object.keys(next).length) { (form.elements.namedItem(Object.keys(next)[0]) as HTMLElement)?.focus(); return; }
    sending.current = true; setBusy(true);
    try { await signIn(mode, email, password); router.replace("/dashboard"); router.refresh(); }
    catch (e) { setFailure(e instanceof Error ? e.message : "Unable to sign in. Please try again."); }
    finally { sending.current = false; setBusy(false); }
  }
  return <main className={styles.shell}><section className={styles.formSide}>
    <p className={styles.eyebrow}>{register ? "Your workspace starts here" : "Your workspace, one sign-in away"}</p>
    <h1 className={styles.title}>{register ? "Create an account" : "Welcome back."}</h1>
    <p className={styles.description}>{register ? "Make room for a clearer view of your services." : "Sign in to continue to Uptime."}</p>
    <form onSubmit={submit} noValidate aria-busy={busy}>
      {failure && <p role="alert" className={styles.alert}>{failure}</p>}
      <div className={styles.field}><label htmlFor="email">Email address</label><input id="email" name="email" type="email" autoComplete="email" placeholder="you@company.com" required aria-invalid={!!errors.email} aria-describedby={errors.email ? "email-error" : undefined} />{errors.email && <p id="email-error" role="alert" className={styles.error}>{errors.email}</p>}</div>
      <div className={styles.field}><label htmlFor="password">Password</label><div className={styles.password}><input id="password" name="password" type={visible ? "text" : "password"} autoComplete={register ? "new-password" : "current-password"} required aria-invalid={!!errors.password} aria-describedby={errors.password ? "password-error" : "password-hint"} /><button type="button" className={styles.reveal} onClick={() => setVisible(!visible)} aria-label={visible ? "Hide password" : "Show password"}>{visible ? "Hide" : "Show"}</button></div>{errors.password ? <p id="password-error" role="alert" className={styles.error}>{errors.password}</p> : <p id="password-hint" className={styles.hint}>12–128 characters. Make it unique.</p>}</div>
      {register && <div className={styles.field}><label htmlFor="confirm">Confirm password</label><input id="confirm" name="confirm" type={visible ? "text" : "password"} autoComplete="new-password" required aria-invalid={!!errors.confirm} aria-describedby={errors.confirm ? "confirm-error" : undefined} />{errors.confirm && <p id="confirm-error" role="alert" className={styles.error}>{errors.confirm}</p>}</div>}
      <button className={styles.primary} disabled={busy} type="submit">{busy ? (register ? "Creating account…" : "Signing in…") : (register ? "Create account" : "Sign in")}</button>
    </form>
    <p className={styles.switch}>{register ? "Already have an account? " : "New to Uptime? "}<Link href={register ? "/login" : "/register"}>{register ? "Sign in" : "Create an account"}</Link></p>
  </section><aside className={styles.aside}><div><p className={styles.eyebrow}>A little more clarity</p><h2>A quieter place<br />to keep watch.</h2><div className={styles.signal} aria-hidden="true">{Array.from({ length: 22 }, (_, i) => <span key={i} />)}</div><p>Your services have a rhythm.<br />Give them a place to be seen.</p></div><div className={styles.asideFoot}>UPTIME / YOUR WORKSPACE</div></aside></main>;
}
