"use client";

import Link from "next/link";
import Image from "next/image";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { Workspace } from "../auth/dashboard";
import { AuthError, loadProfile, updateProfile, uploadAvatar } from "../auth/client";
import type { User } from "../auth/types";
import form from "../auth/auth.module.css";
import styles from "./profile.module.css";

export function Profile() {
  return <Workspace title="Your profile" load={loadProfile}>
    {(user, update) => <ProfileForm key={user.id} user={user} onSaved={update} />}
  </Workspace>;
}

export function ProfileForm({ user, onSaved }: { user: User; onSaved: (user: User) => void }) {
  const router = useRouter();
  const [name, setName] = useState(user.name);
  const [selected, setSelected] = useState<{ file: File; url: string }>();
  const [avatarError, setAvatarError] = useState("");
  const [avatarURL, setAvatarURL] = useState(user.avatar_url);
  const fileInput = useRef<HTMLInputElement>(null);
  useEffect(() => () => { if (selected) URL.revokeObjectURL(selected.url); }, [selected]);
  function selectAvatar(file?: File) {
    if (!file) return;
    setSaved(false); setAvatarError("");
    if (!["image/jpeg", "image/png"].includes(file.type) || !file.size) { setAvatarError("Choose a JPEG or PNG image."); return; }
    if (file.size > 5 * 1024 * 1024) { setAvatarError("Choose an image under 5 MB."); return; }
    setSelected({ file, url: URL.createObjectURL(file) });
  }
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const [error, setError] = useState("");
  const [fieldError, setFieldError] = useState("");
  const [saved, setSaved] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  async function save(event: FormEvent) {
    event.preventDefault();
    if (submitting.current) return;
    setError(""); setFieldError(""); setSaved(false);
    const value = name.trim();
    if ([...value].length > 100 || /\p{Cc}/u.test(value)) {
      setFieldError("Use at most 100 characters without control characters."); input.current?.focus(); return;
    }
    submitting.current = true; setBusy(true);
    try {
      const result = selected ? await uploadAvatar(value, selected.file) : await updateProfile(value);
      setAvatarURL(result.user.avatar_url); setSelected(undefined);
      setName(result.user.name); onSaved(result.user); setSaved(true);
    } catch (e) {
      if (e instanceof AuthError && e.status === 401) { router.replace("/login"); return; }
      setError(e instanceof Error ? e.message : "Unable to save your profile. Please try again.");
    } finally { submitting.current = false; setBusy(false); }
  }
  return <section className={styles.card} aria-label="Profile details">
    <h2>Personal details</h2>
    <p className={form.description}>Choose the name and photo shown in your account.</p>
    <form onSubmit={save} noValidate>
      <div className={styles.avatarSection}>
        <div className={styles.avatarPreview}>
          {selected || avatarURL ? <Image src={selected?.url || avatarURL} alt={selected ? "Selected avatar preview" : "Your avatar"} width={88} height={88} unoptimized /> : <span aria-label="Default avatar">{(name || user.email).charAt(0).toUpperCase()}</span>}
        </div>
        <div>
          <input ref={fileInput} className={styles.fileInput} type="file" accept="image/jpeg,image/png" aria-label="Choose avatar" disabled={busy} onChange={event => { selectAvatar(event.target.files?.[0]); event.target.value = ""; }} />
          <button type="button" className={form.secondary} disabled={busy} onClick={() => fileInput.current?.click()}>Upload avatar</button>
          <p className={form.hint}>JPEG or PNG, up to 5 MB and 2048 × 2048 pixels.</p>
          {selected && <><p className={form.hint}>Preview only. Save changes to update your avatar.</p><button type="button" className={styles.cancelAvatar} disabled={busy} onClick={() => { setSelected(undefined); setAvatarError(""); }}>Cancel selection</button></>}
          {avatarError && <p role="alert" className={form.error}>{avatarError}</p>}
        </div>
      </div>
      <div className={form.field}>
        <label htmlFor="profile-email">Email address</label>
        <input id="profile-email" type="email" value={user.email} readOnly aria-describedby="email-hint" />
        <p id="email-hint" className={form.hint}>Your email address cannot be changed.</p>
      </div>
      <div className={form.field}>
        <label htmlFor="profile-name">Name</label>
        <input ref={input} id="profile-name" autoComplete="name" value={name} disabled={busy} onChange={event => { setName(event.target.value); setSaved(false); setFieldError(""); }} aria-invalid={!!fieldError} aria-describedby={fieldError ? "name-error" : "name-hint"} />
        <p id="name-hint" className={form.hint}>Optional. Up to 100 characters.</p>
        {fieldError && <p id="name-error" className={form.error} role="alert">{fieldError}</p>}
      </div>
      {error && <p className={form.alert} role="alert">{error}</p>}
      {saved && <p role="status" className={styles.success}>Profile saved.</p>}
      <div className={styles.actions}>
        <button type="submit" className={form.primary} disabled={busy}>{busy ? "Saving…" : "Save changes"}</button>
        <Link href="/dashboard">Back to dashboard</Link>
      </div>
    </form>
  </section>;
}
