import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { api, setSessionUser, setToken } from "../api";
import { Icons } from "../components/icons";
import { loginSchema, type LoginValues } from "../lib/schemas";
import "./login.css";

export default function LoginPage() {
  const nav = useNavigate();
  const queryClient = useQueryClient();
  const [showPassword, setShowPassword] = useState(false);
  const { register, handleSubmit, setError, clearErrors, formState: { errors, isSubmitting: loading } } = useForm<LoginValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "" },
  });
  const error = errors.root?.message ?? "";

  const onSubmit = handleSubmit(async ({ email, password }) => {
    clearErrors("root");
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
      setError("root", { message: err instanceof Error ? err.message : "Ошибка входа" });
    }
  });
  return (
    <main className="login-page">
      <div className="login-window">
        <aside className="login-story" aria-label="FIX Gateway">
          <div className="login-brand">
            <img src="/logo.png" alt="" width={45} height={45} className="login-brand-logo" />
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
                <div className="login-network-icon"><img src="/logo.png" alt="" width={30} height={30} /></div>
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
          <form onSubmit={onSubmit} className="login-form" aria-busy={loading} noValidate>
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
                  type="email"
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  placeholder="name@company.com"
                  {...register("email")}
                  aria-invalid={errors.email ? true : undefined}
                  aria-describedby={errors.email ? "login-email-error" : error ? "login-error" : undefined}
                  readOnly={loading}
                  required
                />
                {errors.email && <p id="login-email-error" className="login-field-error">{errors.email.message}</p>}
              </div>

              <div className="login-field">
                <label htmlFor="login-password">Пароль</label>
                <div className="login-password">
                  <input
                    id="login-password"
                    className="login-input"
                    type={showPassword ? "text" : "password"}
                    autoComplete="current-password"
                    placeholder="Введите пароль"
                    {...register("password")}
                    aria-invalid={errors.password ? true : undefined}
                    aria-describedby={errors.password ? "login-password-error" : error ? "login-error" : undefined}
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
                    {showPassword ? <Icons.eyeOff size={18} /> : <Icons.eye size={18} />}
                  </button>
                </div>
                {errors.password && <p id="login-password-error" className="login-field-error">{errors.password.message}</p>}
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
