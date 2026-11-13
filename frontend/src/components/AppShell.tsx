import { useCallback, useEffect, useState, type ReactElement } from "react";
import { Link, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, clearToken, getSessionUser } from "../api";
import { formatDateTime } from "../labels";
import { roleLabel, userInitials, liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { useTheme, type ThemeChoice } from "../lib/theme";
import { cn } from "../lib/utils";
import { Icons } from "./icons";
import { useBreadcrumbState } from "./breadcrumbs";
import type { Notification } from "../types/admin";
import { ConnectionStatus } from "./ConnectionStatus";
import { CommandPalette } from "./CommandPalette";
import { Sheet, SheetContent, SheetTitle } from "./ui/sheet";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./ui/tooltip";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from "./ui/dropdown-menu";
import { BOTTOM_NAV, MAIN_NAV, type NavItem } from "../lib/navigation";

const COLLAPSE_KEY = "fixgw-sidebar-collapsed";

function readCollapsed() {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === "1";
  } catch {
    return false;
  }
}

function NavLink({ item, active, path, collapsed, onNavigate }: { item: NavItem; active: boolean; path: string; collapsed: boolean; onNavigate?: () => void }) {
  const className = cn("nav-link", active && "nav-active");
  const content = (
    <>
      <item.icon size={17} />
      {!collapsed && <span className="truncate">{item.label}</span>}
    </>
  );
  const link = item.href ? (
    <a href={item.href} target="_blank" rel="noreferrer" className={className} aria-label={collapsed ? item.label : undefined}>
      {content}
    </a>
  ) : item.to ? (
    <Link to={item.to} className={className} onClick={onNavigate} aria-current={active ? "page" : undefined} aria-label={collapsed ? item.label : undefined}>
      {content}
    </Link>
  ) : (
    <div className={className}>{content}</div>
  );

  return (
    <div data-section={item.section}>
      {collapsed ? (
        <Tooltip>
          <TooltipTrigger asChild>{link}</TooltipTrigger>
          <TooltipContent side="right">{item.label}</TooltipContent>
        </Tooltip>
      ) : (
        link
      )}
      {item.children && active && !collapsed && (
        <div className="nav-sub">
          {item.children.map((c) => {
            const childActive = path === c.to || (c.to !== "/logs" && path.startsWith(c.to));
            return (
              <Link key={c.to} to={c.to} onClick={onNavigate} aria-current={childActive ? "page" : undefined}>
                {c.label}
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

function SidebarContent({ path, collapsed, onNavigate }: { path: string; collapsed: boolean; onNavigate?: () => void }) {
  return (
    <>
      <Link to="/" className={cn("flex h-14 shrink-0 items-center gap-2.5 border-b border-sidebar-border", collapsed ? "justify-center px-0" : "px-4")} onClick={onNavigate} aria-label={collapsed ? "FIX Gateway" : undefined}>
        <img src="/logo.png" alt="" width={32} height={32} className="h-8 w-8 shrink-0" />
        {!collapsed && (
          <div className="min-w-0 leading-tight">
            <div className="truncate text-[14px] font-semibold tracking-tight text-ink">FIX Gateway</div>
            <div className="truncate text-[11px] text-muted">Операционная консоль</div>
          </div>
        )}
      </Link>

      <nav aria-label="Основная навигация" className={cn("flex-1 space-y-0.5 overflow-y-auto overflow-x-hidden pb-3", collapsed ? "px-2.5" : "px-2")}>
        {SIDEBAR_NAV.map((item, index) => {
          const active = !!(item.to && item.match?.(path));
          const groupStart = SIDEBAR_NAV[index - 1]?.group !== item.group;
          return (
            <div key={item.label}>
              {groupStart && (collapsed ? <div className="mx-2 my-3 h-px bg-sidebar-border" aria-hidden /> : <div className="nav-group-label" data-section={item.section}>{item.group}</div>)}
              {/* Key sections live in the header from 1280px; below that the sidebar keeps them. */}
              <div className={cn(item.header && "xl:hidden")}>
                <NavLink item={item} active={active} path={path} collapsed={collapsed} onNavigate={onNavigate} />
              </div>
            </div>
          );
        })}
      </nav>

      {BOTTOM_NAV.length > 0 && (
        <div className={cn("space-y-0.5 border-t border-sidebar-border py-2", collapsed ? "px-2.5" : "px-2")}>
          {BOTTOM_NAV.map((item) => (
            <NavLink key={item.label} item={item} active={false} path={path} collapsed={collapsed} />
          ))}
        </div>
      )}
    </>
  );
}

const SIDEBAR_NAV = MAIN_NAV.filter((item) => item.sidebar !== false);
const HEADER_NAV = MAIN_NAV.filter((item) => item.header);

/** Key trading sections in the header, shown from 1280px wide. */
function HeaderNav({ path }: { path: string }) {
  return (
    <nav aria-label="Ключевые разделы" className="header-nav hidden xl:flex">
      {HEADER_NAV.map((item) => {
        const active = !!item.match?.(path);
        return (
          <Link key={item.label} to={item.to!} data-section={item.section} className={cn(active && "header-nav-active")} aria-current={active ? "page" : undefined}>
            <item.icon size={15} />
            <span>{item.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}

function UserMenu() {
  const user = getSessionUser();
  const displayName = user?.full_name || user?.username || "Админ";
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <button type="button" className="user-menu-trigger">
              <span aria-hidden className="user-avatar flex h-8 w-8 shrink-0 items-center justify-center text-[12px] font-semibold">
                {userInitials(user?.full_name, user?.username)}
              </span>
              <span className="sr-only">Профиль: {displayName}</span>
            </button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>{displayName}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="min-w-56">
        <DropdownMenuLabel className="font-normal normal-case tracking-normal">
          <div className="truncate text-[13px] font-medium text-ink">{displayName}</div>
          <div className="truncate text-[11px] text-muted">{roleLabel(user?.role)}</div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => {
            clearToken();
            window.location.href = "/login";
          }}
        >
          <Icons.logout size={15} />
          Выйти
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function HeaderButton({ label, children, ...props }: React.ComponentProps<"button"> & { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" className="icon-btn relative" aria-label={label} {...props}>
          {children}
        </button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

const THEMES: Array<{ value: ThemeChoice; label: string; icon: (p: { size?: number }) => ReactElement }> = [
  { value: "light", label: "Светлая", icon: Icons.sun },
  { value: "dark", label: "Тёмная", icon: Icons.moon },
  { value: "system", label: "Как в системе", icon: Icons.monitor },
];

function ThemeMenu() {
  const { theme, resolved, setTheme } = useTheme();
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <button type="button" className="icon-btn" aria-label="Тема оформления">
              {resolved === "dark" ? <Icons.moon size={17} /> : <Icons.sun size={17} />}
            </button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>Тема оформления</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Тема</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={theme} onValueChange={(value) => setTheme(value as ThemeChoice)}>
          {THEMES.map((t) => (
            <DropdownMenuRadioItem key={t.value} value={t.value}>
              <t.icon size={15} />
              {t.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function NotificationBell() {
  const [open, setOpen] = useState(false);
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["notifications"],
    queryFn: () => api<{ notifications: Notification[]; unread: number }>("/api/v1/admin/notifications?unread=1&limit=8"),
    refetchInterval: liveRefetchInterval(connected, 8000),
  });
  const unread = q.data?.unread || 0;
  const items = (q.data?.notifications || []).slice(0, 8);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <button type="button" className="icon-btn relative" aria-label={unread > 0 ? `Уведомления, непрочитанных: ${unread > 99 ? "99+" : unread}` : "Уведомления"}>
              <Icons.bell size={17} />
              {unread > 0 && (
                <span aria-hidden className="absolute right-1 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-semibold leading-none text-white ring-2 ring-[var(--card)]">
                  {unread > 99 ? "99+" : unread}
                </span>
              )}
            </button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>Уведомления</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between border-b border-line px-3 py-2.5">
          <span className="text-[13px] font-semibold">Уведомления</span>
          <Link to="/notifications" className="text-[12px] font-medium text-accent hover:underline" onClick={() => setOpen(false)}>
            Все
          </Link>
        </div>
        <div className="max-h-96 overflow-y-auto p-1.5">
          {items.map((n) => (
            <Link
              key={n.id}
              to="/notifications"
              onClick={() => setOpen(false)}
              className={cn("mb-0.5 block rounded-md px-2.5 py-2 text-[13px] transition-colors hover:bg-subtle", n.severity === "CRITICAL" && "bg-danger-soft hover:bg-danger-soft")}
            >
              <div className="flex items-center gap-2">
                <span className={n.severity === "CRITICAL" || n.severity === "ERROR" ? "badge-danger" : "badge-warn"}>{n.severity}</span>
                {n.count > 1 && <span className="text-[11px] text-muted">×{n.count}</span>}
              </div>
              <div className="mt-1 font-medium">{n.title}</div>
              <div className="text-[11px] text-muted">{formatDateTime(n.last_seen_at)}</div>
            </Link>
          ))}
          {items.length === 0 && <div className="px-2 py-8 text-center text-[13px] text-muted">Нет непрочитанных</div>}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export default function AppShell({ children }: { children: React.ReactNode }) {
  const loc = useLocation();
  const { crumbs: override } = useBreadcrumbState();
  const crumbs = override?.length ? override : breadcrumbs(loc.pathname);
  const [menuOpen, setMenuOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(readCollapsed);
  const [desktop, setDesktop] = useState(() => window.matchMedia("(min-width: 768px)").matches);
  useEffect(() => {
    const media = window.matchMedia("(min-width: 768px)");
    const change = () => { setDesktop(media.matches); if (media.matches) setMenuOpen(false); };
    media.addEventListener("change", change);
    return () => media.removeEventListener("change", change);
  }, []);
  const toggleCollapsed = () => setCollapsed((value) => {
    try { localStorage.setItem(COLLAPSE_KEY, value ? "0" : "1"); } catch { /* storage unavailable */ }
    return !value;
  });
  const [commandOpen, setCommandOpen] = useState(false);
  const closeCommand = useCallback(() => setCommandOpen(false), []);
  useEffect(() => {
    function key(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setCommandOpen(v => !v); }
    }
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, []);
  const closeMenu = () => setMenuOpen(false);
  const narrow = collapsed && desktop;

  return (
    <TooltipProvider>
      <div className="app-shell flex min-h-screen bg-surface font-sans text-ink">
        <a href="#main-content" className="skip-link">К содержимому</a>

        <aside
          id="main-navigation"
          data-collapsed={narrow}
          className="app-sidebar sticky top-0 hidden h-screen shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground md:flex"
        >
          <SidebarContent path={loc.pathname} collapsed={narrow} />
        </aside>

        <Sheet open={menuOpen && !desktop} onOpenChange={setMenuOpen}>
          <SheetContent side="left" className="w-[272px]" aria-describedby={undefined}>
            <SheetTitle className="sr-only">Навигация</SheetTitle>
            <SidebarContent path={loc.pathname} collapsed={false} onNavigate={closeMenu} />
          </SheetContent>
        </Sheet>

        <div className="flex min-w-0 flex-1 flex-col">
          <header className="app-header sticky top-0 z-20 flex items-center justify-between gap-3 border-b border-line px-4 md:px-6">
            <div className="flex min-w-0 items-center gap-2">
              {desktop ? (
                <HeaderButton label={collapsed ? "Развернуть меню" : "Свернуть меню"} aria-controls="main-navigation" aria-expanded={!collapsed} onClick={toggleCollapsed}>
                  <Icons.panel size={17} />
                </HeaderButton>
              ) : (
                <button type="button" className="icon-btn" aria-label="Меню" aria-expanded={menuOpen} onClick={() => setMenuOpen(true)}>
                  <Icons.menu size={18} />
                </button>
              )}
              {/* One crumb only repeats the page heading; crumbs show from the second level (a card in a list). */}
              {crumbs.length > 1 && <div className="mx-1 hidden h-5 w-px bg-line md:block" aria-hidden />}
              {crumbs.length > 1 && <nav aria-label="Хлебные крошки" className="min-w-0">
                <ol className="flex min-w-0 items-center gap-1.5 text-[13px] text-muted">
                  {crumbs.map((c, i) => (
                    <li key={c.label} className="flex min-w-0 items-center gap-1.5">
                      {i > 0 && <Icons.chevron size={14} className="shrink-0 text-faint" />}
                      {c.to ? (
                        <Link to={c.to} className="truncate transition-colors hover:text-ink">
                          {c.label}
                        </Link>
                      ) : (
                        <span className={cn("truncate", i === crumbs.length - 1 && "font-medium text-ink")} aria-current={i === crumbs.length - 1 ? "page" : undefined}>{c.label}</span>
                      )}
                    </li>
                  ))}
                </ol>
              </nav>}
            </div>
            <HeaderNav path={loc.pathname} />
            <div className="flex shrink-0 items-center gap-1">
              <button type="button" className="global-search mr-1" onClick={() => setCommandOpen(true)} aria-keyshortcuts="Control+K" aria-label="Быстрый переход">
                <Icons.search size={15} />
                <span>Быстрый переход</span>
                <kbd>Ctrl K</kbd>
              </button>
              <ThemeMenu />
              <NotificationBell />
              <div className="mx-1 h-5 w-px bg-line" aria-hidden />
              <UserMenu />
            </div>
          </header>
          <main id="main-content" className="app-main flex-1 px-4 py-6 md:px-6">{children}</main>
          <ConnectionStatus />
        </div>
        {commandOpen && <CommandPalette items={MAIN_NAV} onClose={closeCommand} />}
      </div>
    </TooltipProvider>
  );
}

function breadcrumbs(pathname: string): Array<{ label: string; to?: string }> {
  if (pathname === "/grpc") return [{ label: "gRPC · Подписки" }];
  if (pathname === "/monitoring") return [{ label: "Мониторинг" }];
  if (pathname === "/rate-limits") return [{ label: "Rate Limits" }];
  if (pathname === "/classes") return [{ label: "Классы QUIK" }];
  if (pathname === "/execution-inbox") return [{ label: "Входящие FIX" }];
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
