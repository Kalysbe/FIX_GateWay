import { useEffect, useState, type FormEvent } from "react";
import { api, getSessionUser, getToken, redirectToLogin, sessionExpiresAt, setSessionUser, setToken, watchSessionExpiry } from "../api";
import { Modal } from "./Modal";
import { Icons } from "./icons";
import { useToast } from "./Toast";

/** How long before the session ends the console asks to extend it. */
export const SESSION_WARN_MS = 5 * 60_000;

const clock = (ms: number) => {
  const s = Math.max(0, Math.ceil(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
};

/**
 * Warns five minutes before the admin session ends and extends it in place:
 * the password is checked by the usual login, the new token replaces the old
 * one and the page, its filters and anything typed into a form stay as they are.
 */
export function SessionGuard() {
  const toast = useToast();
  const [token, setCurrent] = useState(getToken);
  const [now, setNow] = useState(Date.now);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [dismissedFor, setDismissedFor] = useState("");
  const expiresAt = sessionExpiresAt(token);
  const left = expiresAt ? expiresAt - now : Infinity;
  const warn = left <= SESSION_WARN_MS && left > 0 && dismissedFor !== token;
  const email = getSessionUser()?.email ?? "";

  // The current token's own expiry timer; a replaced token's timer does nothing.
  useEffect(() => watchSessionExpiry(), [token]);
  useEffect(() => {
    if (!expiresAt) return;
    // Tick each second only while the warning is close; otherwise wake when it is due.
    const due = expiresAt - SESSION_WARN_MS - Date.now();
    if (due > 0) {
      const id = window.setTimeout(() => setNow(Date.now()), Math.min(due, 2 ** 31 - 1));
      return () => window.clearTimeout(id);
    }
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [expiresAt, now]);

  async function extend(e: FormEvent) {
    e.preventDefault();
    if (!password || busy) return;
    setBusy(true);
    setError("");
    try {
      const res = await api<{ token: string; user?: { id?: string; email?: string; role?: string; full_name?: string } }>(
        "/api/v1/admin/login", { method: "POST", body: JSON.stringify({ email, password }) });
      setToken(res.token);
      if (res.user) setSessionUser(res.user);
      setPassword("");
      setCurrent(res.token);
      setNow(Date.now());
      toast.push("Сессия продлена", "ok");
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : "Не удалось продлить сессию");
    } finally {
      setBusy(false);
    }
  }

  if (!warn) return null;
  return <Modal title="Сессия скоро закончится" onClose={() => setDismissedFor(token)}>
    <form onSubmit={extend} className="space-y-3" noValidate>
      <p className="text-sm text-muted">
        Через <strong className="font-mono text-ink" aria-live="polite">{clock(left)}</strong> консоль выйдет из учётной записи {email && <span className="font-medium text-ink">{email}</span>}.
        Чтобы продолжить работу на этой странице, введите пароль: открытые фильтры и введённые данные сохранятся.
      </p>
      <label className="block space-y-1.5 text-sm">
        <span className="text-muted">Пароль</span>
        <input className="input" type="password" autoComplete="current-password" data-autofocus value={password} onChange={e => setPassword(e.target.value)} aria-invalid={error ? true : undefined} />
      </label>
      {error && <p role="alert" className="rounded-md border border-danger/20 bg-danger-soft px-3 py-2 text-sm text-danger">{error}</p>}
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className="btn-muted" onClick={() => redirectToLogin()}>Выйти</button>
        <button type="button" className="btn-muted" onClick={() => setDismissedFor(token)}>Напомнить позже</button>
        <button type="submit" className="btn" disabled={!password || busy}>{busy && <Icons.refresh size={14} className="animate-spin" />}Продлить сессию</button>
      </div>
    </form>
  </Modal>;
}
