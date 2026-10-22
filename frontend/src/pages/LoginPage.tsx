import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { api, setSessionUser, setToken } from "../api";
import { Icons } from "../components/icons";
import "./login.css";

export default function LoginPage() {
  const nav = useNavigate();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (loading) return;
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
    <main className="login-page">
      <div className="login-window">
        <aside className="login-story" aria-label="FIX Gateway">
          <div className="login-brand">
            <div className="login-brand-mark"><Icons.logo size={25} /></div>
            <div>
              <strong>FIX Gateway</strong>
              <span>Операционная консоль</span>
            </div>
          </div>

          <div className="login-story-content">
            <div className="login-eyebrow">Единая точка управления</div>
            <h2>Торговый поток.<br /><span>Под вашим<br />контролем.</span></h2>
            <p>Заявки, сделки и подключения —<br className="login-desktop-break" /> в одном рабочем пространстве.</p>

            <div className="login-network" aria-hidden="true">
              <div className="login-network-node">
                <div className="login-network-icon"><Icons.brokers size={21} /></div>
                <span>Брокеры</span>
              </div>
              <div className="login-network-line"><Icons.chevron size={12} /></div>
              <div className="login-network-node login-network-gateway">
                <div className="login-network-icon"><Icons.logo size={27} /></div>
                <span>Gateway</span>
              </div>
              <div className="login-network-line"><Icons.chevron size={12} /></div>
              <div className="login-network-node">
                <div className="login-network-icon"><Icons.adapters size={21} /></div>
                <span>QUIK</span>
              </div>
            </div>
          </div>

          <div className="login-story-footer"><span />Инфраструктура торговых операций</div>
        </aside>

        <section className="login-form-area" aria-labelledby="login-title">
          <form onSubmit={onSubmit} className="login-form" aria-busy={loading}>
            <div className="login-form-heading">
              <div className="login-form-symbol"><Icons.lock size={23} /></div>
              <h1 id="login-title">Вход в консоль</h1>
              <p>Добро пожаловать.<br /> Войдите в свою учётную запись.</p>
            </div>

            <div className="login-fields">
              <div className="login-field">
                <label htmlFor="login-email">Электронная почта</label>
                <input
                  id="login-email"
                  className="login-input"
                  name="email"
                  type="email"
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  placeholder="name@company.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  aria-describedby={error ? "login-error" : undefined}
                  readOnly={loading}
                  required
                />
              </div>

              <div className="login-field">
                <label htmlFor="login-password">Пароль</label>
                <div className="login-password">
                  <input
                    id="login-password"
                    className="login-input"
                    type={showPassword ? "text" : "password"}
                    name="password"
                    autoComplete="current-password"
                    placeholder="Введите пароль"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    aria-describedby={error ? "login-error" : undefined}
                    readOnly={loading}
                    required
                  />
                  <button
                    className="login-password-toggle"
                    type="button"
                    onClick={() => setShowPassword((visible) => !visible)}
                    aria-label={showPassword ? "Скрыть пароль" : "Показать пароль"}
                    aria-controls="login-password"
                    title={showPassword ? "Скрыть пароль" : "Показать пароль"}
                  >
                    <Icons.eye size={19} />
                    {showPassword && <span className="login-eye-slash" aria-hidden="true" />}
                  </button>
                </div>
              </div>
            </div>

            {error && <div id="login-error" role="alert" className="login-error"><Icons.info size={17} /><span>{error}</span></div>}

            <button className="login-submit" type="submit" disabled={loading} aria-live="polite">
              <span>{loading ? "Выполняется вход…" : "Войти в консоль"}</span>
              {loading ? <span className="login-spinner" aria-hidden="true" /> : <Icons.chevron size={18} />}
            </button>

            <div className="login-form-footer"><Icons.lock size={13} /><span>Доступ для авторизованных пользователей</span></div>
          </form>
        </section>
      </div>
    </main>
  );
}
