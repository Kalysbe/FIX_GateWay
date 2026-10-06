import { useEffect, useRef, useState, type ReactElement } from "react";
import { Link, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, clearToken, getSessionUser } from "../api";
import { formatDateTime } from "../labels";
import { roleLabel, userInitials, liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { Icons } from "./icons";
import { useBreadcrumbState } from "./breadcrumbs";
import type { Notification } from "../types/admin";

type NavItem = {
  to?: string;
  href?: string;
  label: string;
  icon: (p: { size?: number; className?: string }) => ReactElement;
  match?: (path: string) => boolean;
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
  { to: "/logs/audit", label: "Аудит" },
];

const MAIN_NAV: NavItem[] = [
  { to: "/rate-limits", label: "Rate Limits", icon: Icons.dashboard, match: (p) => p === "/rate-limits" },
  { to: "/monitoring", label: "Мониторинг", icon: Icons.dashboard, match: (p) => p === "/monitoring" },
  { to: "/", label: "Дашборд", icon: Icons.dashboard, match: (p) => p === "/" },
  { to: "/brokers", label: "Брокеры", icon: Icons.brokers, match: (p) => p.startsWith("/brokers") },
  { to: "/tokens", label: "Токены", icon: Icons.tokens, match: (p) => p.startsWith("/tokens") },
  { to: "/orders", label: "Заявки", icon: Icons.orders, match: (p) => p.startsWith("/orders") },
  { to: "/trades", label: "Сделки", icon: Icons.trades, match: (p) => p.startsWith("/trades") },
  { to: "/market-data", label: "Текущие торги", icon: Icons.quotes, match: (p) => p.startsWith("/market-data") },
  { to: "/quotes", label: "Котировки", icon: Icons.quotes, match: (p) => p.startsWith("/quotes") },
  { to: "/positions", label: "Позиции", icon: Icons.accounts, match: (p) => p.startsWith("/positions") },
  { to: "/reference", label: "Инструменты", icon: Icons.instruments, match: (p) => p.startsWith("/reference") },
  { to: "/adapters", label: "Адаптеры", icon: Icons.adapters, match: (p) => p.startsWith("/adapters") },
  { to: "/logs", label: "Логи", icon: Icons.audit, match: (p) => p.startsWith("/logs"), children: LOG_CHILDREN },
  { to: "/notifications", label: "Уведомления", icon: Icons.bell, match: (p) => p.startsWith("/notifications") },
];

const BOTTOM_NAV: NavItem[] = [{ href: "/swagger", label: "API-документация", icon: Icons.docs }];

function NavLink({ item, active, path, onNavigate }: { item: NavItem; active: boolean; path: string; onNavigate?: () => void }) {
  const className = [
    "flex items-center gap-3 rounded-md px-3 py-2 text-[13px] font-medium transition-colors",
    active ? "bg-accent/15 text-accent" : "text-slate-600 hover:bg-slate-100 hover:text-ink",
  ].join(" ");

  const content = (
    <>
      <item.icon size={18} className={active ? "text-accent" : "text-slate-400"} />
      <span className="truncate">{item.label}</span>
    </>
  );

  return (
    <div>
      {item.href ? (
        <a href={item.href} target="_blank" rel="noreferrer" className={className}>
          {content}
        </a>
      ) : item.to ? (
        <Link to={item.to} className={className} onClick={onNavigate}>
          {content}
        </Link>
      ) : (
        <div className={className}>{content}</div>
      )}
      {item.children && active && (
        <div className="mb-1 ml-7 mt-0.5 space-y-0.5">
          {item.children.map((c) => {
            const childActive = path === c.to || (c.to !== "/logs" && path.startsWith(c.to));
            return (
              <Link
                key={c.to}
                to={c.to}
                onClick={onNavigate}
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

function NotificationBell() {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["notifications"],
    queryFn: () => api<{ notifications: Notification[]; unread: number }>("/api/v1/admin/notifications?unread=1&limit=8"),
    refetchInterval: liveRefetchInterval(connected, 8000),
  });
  const unread = q.data?.unread || 0;

  useEffect(() => {
    if (!open) return;
    function onDoc(e: MouseEvent) {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className="relative" ref={wrapRef}>
      <button
        type="button"
        className="icon-btn relative"
        title="Уведомления"
        aria-label="Уведомления"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <Icons.bell size={18} />
        {unread > 0 && (
          <span className="absolute right-1 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-semibold text-white">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-30 mt-2 w-80 rounded-lg border border-line bg-panel p-2 shadow-lg">
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
              className={`mb-1 block rounded-md px-2 py-2 text-sm hover:bg-slate-50 ${
                n.severity === "CRITICAL" ? "bg-rose-50" : ""
              }`}
            >
              <div className="flex items-center gap-2">
                <span className={n.severity === "CRITICAL" || n.severity === "ERROR" ? "badge-danger" : "badge-warn"}>
                  {n.severity}
                </span>
                {n.count > 1 && <span className="text-[11px] text-muted">×{n.count}</span>}
              </div>
              <div className="mt-0.5 font-medium">{n.title}</div>
              <div className="text-[11px] text-muted">{formatDateTime(n.last_seen_at)}</div>
            </Link>
          ))}
          {(q.data?.notifications || []).length === 0 && (
            <div className="px-2 py-6 text-center text-sm text-muted">Нет непрочитанных</div>
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
  const user = getSessionUser();
  const [menuOpen, setMenuOpen] = useState(false);
  const displayName = user?.full_name || user?.email || "Админ";
  const closeMenu = () => setMenuOpen(false);

  return (
    <div className="flex min-h-screen bg-surface font-sans text-ink">
      {menuOpen && (
        <button
          type="button"
          className="fixed inset-0 z-30 bg-slate-900/40 md:hidden"
          aria-label="Закрыть меню"
          onClick={closeMenu}
        />
      )}
      <aside
        className={`fixed inset-y-0 left-0 z-40 flex w-[240px] shrink-0 flex-col border-r border-line bg-white text-ink transition-transform md:sticky md:top-0 md:h-screen md:translate-x-0 ${
          menuOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        <div className="flex items-center gap-2.5 px-4 py-5">
          <div className="flex h-9 w-9 items-center justify-center rounded-md bg-accent text-white">
            <Icons.logo size={16} />
          </div>
          <div className="min-w-0 leading-tight">
            <div className="truncate text-[14px] font-semibold tracking-tight text-ink">FIX Gateway</div>
            <div className="truncate text-[11px] text-muted">Операционная консоль</div>
          </div>
        </div>

        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 pb-3">
          {MAIN_NAV.map((item) => {
            const active = !!(item.to && item.match?.(loc.pathname));
            return <NavLink key={item.label} item={item} active={active} path={loc.pathname} onNavigate={closeMenu} />;
          })}
        </nav>

        <div className="space-y-0.5 border-t border-slate-100 px-2 py-3">
          {BOTTOM_NAV.map((item) => (
            <NavLink key={item.label} item={item} active={false} path={loc.pathname} />
          ))}
        </div>

        <div className="border-t border-slate-100 px-3 py-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-9 w-9 items-center justify-center rounded-md bg-accent text-sm font-semibold text-white">
              {userInitials(user?.full_name, user?.email)}
            </div>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{displayName}</div>
              <div className="truncate text-[11px] text-muted">{roleLabel(user?.role)}</div>
            </div>
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-14 items-center justify-between border-b border-line bg-white/95 px-4 backdrop-blur md:px-6">
          <div className="flex min-w-0 items-center gap-2 text-sm text-muted">
            <button type="button" className="icon-btn md:hidden" aria-label="Меню" onClick={() => setMenuOpen(true)}>
              <Icons.menu size={18} />
            </button>
            {crumbs.map((c, i) => (
              <span key={c.label} className="flex items-center gap-2">
                {i > 0 && <span className="text-line">/</span>}
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
        <main className="flex-1 px-4 py-5 md:px-6">{children}</main>
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
  if (pathname.startsWith("/quotes")) return [{ label: "Котировки" }];
  if (pathname.startsWith("/positions")) return [{ label: "Позиции" }];
  if (pathname.startsWith("/adapters")) return [{ label: "Адаптеры" }];
  if (pathname.startsWith("/reference")) return [{ label: "Инструменты" }];
  if (pathname.startsWith("/logs")) return [{ label: "Логи" }];
  if (pathname.startsWith("/notifications")) return [{ label: "Уведомления" }];
  return [{ label: "Админ" }];
}
