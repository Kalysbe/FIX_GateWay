import { useEffect, useState, type ReactElement } from "react";
import { Link, useLocation } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, clearToken, getToken, isSessionValid, redirectToLogin } from "../api";
import { formatDateTime } from "../labels";
import { Icons } from "./icons";
import { useBreadcrumbState } from "./breadcrumbs";

type NavItem = {
  to?: string;
  label: string;
  icon: (p: { size?: number; className?: string }) => ReactElement;
  match?: (path: string) => boolean;
  soon?: boolean;
  children?: Array<{ to: string; label: string }>;
};

const LOG_CHILDREN = [
  { to: "/logs", label: "Все" },
  { to: "/logs/api", label: "API" },
  { to: "/logs/orders", label: "Заявки" },
  { to: "/logs/errors", label: "Ошибки" },
  { to: "/logs/auth", label: "Авторизация" },
  { to: "/logs/fix", label: "FIX / Adapters" },
  { to: "/logs/system", label: "Система" },
  { to: "/logs/audit", label: "Audit" },
];

const MAIN_NAV: NavItem[] = [
  { to: "/", label: "Дашборд", icon: Icons.dashboard, match: (p) => p === "/" },
  { to: "/brokers", label: "Брокеры", icon: Icons.brokers, match: (p) => p.startsWith("/brokers") },
  { to: "/tokens", label: "Токены", icon: Icons.tokens, match: (p) => p.startsWith("/tokens") },
  { to: "/orders", label: "Заявки", icon: Icons.orders, match: (p) => p.startsWith("/orders") },
  { to: "/trades", label: "Сделки", icon: Icons.trades, match: (p) => p.startsWith("/trades") },
  { to: "/market-data", label: "Текущие торги", icon: Icons.quotes, match: (p) => p.startsWith("/market-data") },
  { to: "/positions", label: "Позиции", icon: Icons.accounts, match: (p) => p.startsWith("/positions") },
  { to: "/reference", label: "Инструменты", icon: Icons.instruments, match: (p) => p.startsWith("/reference") },
  { to: "/adapters", label: "Адаптеры", icon: Icons.adapters, match: (p) => p.startsWith("/adapters") },
  { to: "/", label: "Мониторинг", icon: Icons.monitoring, match: () => false },
  { label: "Пользователи", icon: Icons.users, soon: true },
  { label: "Роли и права", icon: Icons.roles, soon: true },
  { to: "/logs", label: "Логи", icon: Icons.audit, match: (p) => p.startsWith("/logs"), children: LOG_CHILDREN },
  { to: "/notifications", label: "Уведомления", icon: Icons.bell, match: (p) => p.startsWith("/notifications") },
];

const BOTTOM_NAV: NavItem[] = [
  { label: "Настройки", icon: Icons.settings, soon: true },
  { label: "API Документация", icon: Icons.docs, soon: true },
];

function NavLink({ item, active, path }: { item: NavItem; active: boolean; path: string }) {
  const className = [
    "flex items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium transition-colors",
    active
      ? "bg-accent/15 text-accent"
      : item.soon
        ? "cursor-default text-slate-400"
        : "text-slate-600 hover:bg-slate-100 hover:text-ink",
  ].join(" ");

  const content = (
    <>
      <item.icon size={18} className={active ? "text-accent" : "text-slate-400"} />
      <span className="truncate">{item.label}</span>
      {item.soon && <span className="ml-auto text-[10px] uppercase tracking-wide text-slate-300">soon</span>}
    </>
  );

  return (
    <div>
      {!item.to || item.soon ? (
        <div className={className}>{content}</div>
      ) : (
        <Link to={item.to} className={className}>
          {content}
        </Link>
      )}
      {item.children && active && (
        <div className="mb-1 ml-7 mt-0.5 space-y-0.5">
          {item.children.map((c) => {
            const childActive = path === c.to || (c.to !== "/logs" && path.startsWith(c.to));
            return (
              <Link
                key={c.to}
                to={c.to}
                className={`block rounded-md px-2 py-1 text-[12px] ${
                  childActive ? "font-medium text-accent" : "text-slate-500 hover:text-ink"
                }`}
              >
                {c.label}
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

type Note = {
  id: string;
  severity: string;
  title: string;
  count: number;
  last_seen_at: string;
  read_at?: string | null;
};

function NotificationBell() {
  const [open, setOpen] = useState(false);
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["notifications"],
    queryFn: () => api<{ notifications: Note[]; unread: number }>("/api/v1/admin/notifications?unread=1&limit=8"),
    refetchInterval: 8000,
  });
  const unread = q.data?.unread || 0;

  useEffect(() => {
    if (!isSessionValid()) return;
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const host = import.meta.env.VITE_API_URL ? new URL(import.meta.env.VITE_API_URL).host : location.host;
    const ws = new WebSocket(`${proto}://${host}/api/v1/admin/ws?token=${getToken()}`);
    let refreshTimer: ReturnType<typeof setTimeout> | undefined;
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        if (msg.type !== "notification.new") return;
        clearTimeout(refreshTimer);
        refreshTimer = setTimeout(() => {
          qc.invalidateQueries({ queryKey: ["notifications"] });
        }, 400);
      } catch {
        /* ignore */
      }
    };
    ws.onclose = () => {
      if (!isSessionValid()) redirectToLogin();
    };
    return () => {
      clearTimeout(refreshTimer);
      ws.close();
    };
  }, [qc]);

  return (
    <div className="relative">
      <button type="button" className="icon-btn relative" title="Уведомления" aria-label="Уведомления" onClick={() => setOpen((v) => !v)}>
        <Icons.bell size={18} />
        {unread > 0 && (
          <span className="absolute right-1 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-semibold text-white">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-30 mt-2 w-80 rounded-xl border border-slate-200 bg-white p-2 shadow-lg">
          <div className="mb-1 flex items-center justify-between px-2 py-1">
            <span className="text-sm font-medium">Уведомления</span>
            <Link to="/notifications" className="text-xs text-accent" onClick={() => setOpen(false)}>
              Все
            </Link>
          </div>
          {(q.data?.notifications || []).slice(0, 8).map((n) => (
            <Link
              key={n.id}
              to="/notifications"
              onClick={() => setOpen(false)}
              className={`mb-1 block rounded-lg px-2 py-2 text-sm hover:bg-slate-50 ${
                n.severity === "CRITICAL" ? "bg-rose-50" : ""
              }`}
            >
              <div className="flex items-center gap-2">
                <span className={n.severity === "CRITICAL" || n.severity === "ERROR" ? "badge-danger" : "badge-warn"}>
                  {n.severity}
                </span>
                {n.count > 1 && <span className="text-[11px] text-slate-400">×{n.count}</span>}
              </div>
              <div className="mt-0.5 font-medium">{n.title}</div>
              <div className="text-[11px] text-slate-400">{formatDateTime(n.last_seen_at)}</div>
            </Link>
          ))}
          {(q.data?.notifications || []).length === 0 && (
            <div className="px-2 py-6 text-center text-sm text-slate-400">Нет непрочитанных</div>
          )}
        </div>
      )}
    </div>
  );
}

export default function AppShell({ children }: { children: React.ReactNode }) {
  const loc = useLocation();
  const { crumbs: override } = useBreadcrumbState();
  const crumbs = override?.length ? override : breadcrumbs(loc.pathname);

  return (
    <div className="flex min-h-screen bg-surface text-ink">
      <aside className="sticky top-0 flex h-screen w-[232px] shrink-0 flex-col border-r border-slate-200 bg-white">
        <div className="flex items-center gap-2.5 px-4 py-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent text-white">
            <Icons.logo size={16} />
          </div>
          <div className="min-w-0 leading-tight">
            <div className="truncate text-[13px] font-semibold text-ink">FIX Gateway</div>
            <div className="truncate text-[11px] text-slate-500">Broker CRM</div>
          </div>
        </div>

        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 pb-3">
          {MAIN_NAV.map((item) => {
            const active = !!(item.to && item.match?.(loc.pathname));
            return <NavLink key={item.label} item={item} active={active} path={loc.pathname} />;
          })}
        </nav>

        <div className="space-y-0.5 border-t border-slate-100 px-2 py-3">
          {BOTTOM_NAV.map((item) => (
            <NavLink key={item.label} item={item} active={false} path={loc.pathname} />
          ))}
        </div>

        <div className="border-t border-slate-100 px-3 py-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-9 w-9 items-center justify-center rounded-full bg-accent text-sm font-semibold text-white">
              A
            </div>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">Admin</div>
              <div className="truncate text-[11px] text-slate-500">Суперадминистратор</div>
            </div>
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-14 items-center justify-between border-b border-slate-200 bg-white/95 px-6 backdrop-blur">
          <div className="flex items-center gap-2 text-sm text-slate-500">
            {crumbs.map((c, i) => (
              <span key={c.label} className="flex items-center gap-2">
                {i > 0 && <span className="text-slate-300">›</span>}
                {c.to ? (
                  <Link to={c.to} className="hover:text-accent">
                    {c.label}
                  </Link>
                ) : (
                  <span className={i === crumbs.length - 1 ? "font-medium text-ink" : ""}>{c.label}</span>
                )}
              </span>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <button type="button" className="icon-btn" title="Тема" aria-label="Тема">
              <Icons.sun size={18} />
            </button>
            <NotificationBell />
            <button
              type="button"
              className="btn-muted ml-1 gap-1.5"
              onClick={() => {
                clearToken();
                window.location.href = "/login";
              }}
            >
              <Icons.logout size={16} />
              Выйти
            </button>
          </div>
        </header>
        <main className="flex-1 px-6 py-5">{children}</main>
      </div>
    </div>
  );
}

function breadcrumbs(pathname: string): Array<{ label: string; to?: string }> {
  if (pathname === "/") return [{ label: "Дашборд" }];
  if (pathname.startsWith("/brokers/")) {
    return [{ label: "Брокеры", to: "/brokers" }, { label: "Карточка брокера" }];
  }
  if (pathname.startsWith("/brokers")) return [{ label: "Брокеры" }];
  if (pathname.startsWith("/tokens")) return [{ label: "Токены" }];
  if (pathname.startsWith("/orders/")) {
    return [{ label: "Заявки", to: "/orders" }, { label: "Детали заявки" }];
  }
  if (pathname.startsWith("/orders")) return [{ label: "Заявки" }];
  if (pathname.startsWith("/trades")) return [{ label: "Сделки" }];
  if (pathname.startsWith("/market-data")) return [{ label: "Текущие торги" }];
  if (pathname.startsWith("/positions")) return [{ label: "Позиции" }];
  if (pathname.startsWith("/adapters")) return [{ label: "Адаптеры" }];
  if (pathname.startsWith("/reference")) return [{ label: "Инструменты" }];
  if (pathname.startsWith("/logs")) return [{ label: "Логи" }];
  if (pathname.startsWith("/notifications")) return [{ label: "Уведомления" }];
  return [{ label: "Админ" }];
}
