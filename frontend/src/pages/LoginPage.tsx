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
    <div className="login-page">
      <aside className="login-story">
        <div className="flex items-center gap-3"><div className="brand-mark flex h-10 w-10 items-center justify-center text-white"><Icons.logo size={21} /></div><div><strong className="text-base">FIX Gateway</strong><div className="text-[11px] text-muted">Операционная консоль</div></div></div>
        <div><div className="login-eyebrow mb-5">Контроль. Скорость. Прозрачность.</div><h2>Весь торговый поток.<br /><span className="text-accent">В одном пространстве.</span></h2><p>Заявки, сделки и рыночные данные. Наблюдайте за подключениями и управляйте доступом из единой консоли.</p><div className="login-route"><span>Брокер</span><Icons.chevron size={16} /><span>FIX Gateway</span><Icons.chevron size={16} /><span>QUIK</span></div></div>
        <div className="text-[11px] text-muted">FIX / QUIK · HTTP API · gRPC Stream</div>
      </aside>
      <div className="login-form-area"><form onSubmit={onSubmit} className="login-form space-y-5">
        <div><div className="login-eyebrow">FIX Gateway</div><h1>Вход в консоль</h1><p className="text-sm text-muted">Используйте вашу учётную запись</p></div>
        <label className="block space-y-1.5 text-sm">
          <span className="text-muted">Email</span>
          <input className="input" name="email" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label className="block space-y-1.5 text-sm">
          <span className="text-muted">Пароль</span>
          <input
            className="input"
            type="password"
            name="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        {error && <div role="alert" className="rounded-lg border border-rose-100 bg-rose-50 p-3 text-sm text-danger">{error}</div>}
        <button className="btn w-full py-2.5" disabled={loading}>
          {loading ? "Вход…" : "Войти"}
        </button>
        <div className="flex items-center justify-center gap-1.5 pt-3 text-[11px] text-muted"><Icons.lock size={12} />Доступ для авторизованных пользователей</div>
      </form></div>
    </div>
  );
}
