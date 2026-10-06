import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { api, setSessionUser, setToken } from "../api";
import { Icons } from "../components/icons";

export default function LoginPage() {
  const nav = useNavigate();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await api<{ token: string; user?: { id?: string; email?: string; role?: string; full_name?: string } }>(
        "/api/v1/admin/login",
        { method: "POST", body: JSON.stringify({ email, password }) },
      );
      // Cached admin data belongs to the previous authenticated session.
      await queryClient.cancelQueries();
      queryClient.clear();
      setToken(res.token);
      if (res.user) setSessionUser(res.user);
      nav("/");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Ошибка входа");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-surface px-4">
      <form onSubmit={onSubmit} className="card w-full max-w-md space-y-4">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-white">
            <Icons.logo size={18} />
          </div>
          <div>
            <h1 className="text-xl font-semibold tracking-tight">FIX Gateway</h1>
            <p className="text-sm text-muted">Вход в операционную консоль</p>
          </div>
        </div>
        <label className="block space-y-1.5 text-sm">
          <span className="text-muted">Email</span>
          <input className="input" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label className="block space-y-1.5 text-sm">
          <span className="text-muted">Пароль</span>
          <input
            className="input"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        {error && <div className="text-sm text-danger">{error}</div>}
        <button className="btn w-full py-2.5" disabled={loading}>
          {loading ? "Вход…" : "Войти"}
        </button>
      </form>
    </div>
  );
}
