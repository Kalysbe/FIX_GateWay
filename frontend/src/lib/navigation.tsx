import type { ReactElement } from "react";
import { Icons } from "../components/icons";

/** Each part of the console has its own hue: menu icons, the active item, page headings, dashboard tiles. */
export type Section = "overview" | "trading" | "admin" | "infra";

export type NavItem = {
  group?: string;
  section?: Section;
  to?: string;
  href?: string;
  label: string;
  icon: (p: { size?: number; className?: string }) => ReactElement;
  match?: (path: string) => boolean;
  children?: Array<{ to: string; label: string }>;
  /** Key section: shown in the header on wide screens, where the sidebar drops it. */
  header?: boolean;
  /** Reached from the header only (the bell): not repeated in the sidebar. */
  sidebar?: false;
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

export const MAIN_NAV: NavItem[] = [
  { group: "Обзор", section: "overview", to: "/", label: "Дашборд", icon: Icons.dashboard, match: (p) => p === "/" },
  { group: "Обзор", section: "overview", to: "/monitoring", label: "Мониторинг", icon: Icons.monitoring, match: (p) => p === "/monitoring" },
  { group: "Торговые операции", section: "trading", to: "/orders", label: "Заявки", icon: Icons.orders, match: (p) => p.startsWith("/orders"), header: true },
  { group: "Торговые операции", section: "trading", to: "/trades", label: "Сделки", icon: Icons.trades, match: (p) => p.startsWith("/trades"), header: true },
  { group: "Торговые операции", section: "trading", to: "/market-data", label: "Текущие торги", icon: Icons.quotes, match: (p) => p.startsWith("/market-data"), header: true },
  { group: "Торговые операции", section: "trading", to: "/quotes", label: "Котировки", icon: Icons.chart, match: (p) => p.startsWith("/quotes"), header: true },
  { group: "Торговые операции", section: "trading", to: "/positions", label: "Позиции", icon: Icons.accounts, match: (p) => p.startsWith("/positions"), header: true },
  { group: "Торговые операции", section: "trading", to: "/reference", label: "Инструменты", icon: Icons.instruments, match: (p) => p.startsWith("/reference") },
  { group: "Управление", section: "admin", to: "/brokers", label: "Брокеры", icon: Icons.brokers, match: (p) => p.startsWith("/brokers") },
  { group: "Управление", section: "admin", to: "/tokens", label: "Токены", icon: Icons.tokens, match: (p) => p.startsWith("/tokens") },
  { group: "Управление", section: "admin", to: "/classes", label: "Классы QUIK", icon: Icons.settings, match: (p) => p === "/classes" },
  { group: "Управление", section: "admin", to: "/rate-limits", label: "Rate Limits", icon: Icons.roles, match: (p) => p === "/rate-limits" },
  { group: "Инфраструктура", section: "infra", to: "/adapters", label: "Подключения FIX", icon: Icons.adapters, match: (p) => p.startsWith("/adapters") },
  { group: "Инфраструктура", section: "infra", to: "/execution-inbox", label: "Входящие FIX", icon: Icons.inbox, match: (p) => p === "/execution-inbox" },
  { group: "Инфраструктура", section: "infra", to: "/grpc", label: "gRPC · Подписки", icon: Icons.stream, match: (p) => p === "/grpc" },
  { group: "Инфраструктура", section: "infra", to: "/logs", label: "Логи и аудит", icon: Icons.audit, match: (p) => p.startsWith("/logs"), children: LOG_CHILDREN },
  { group: "Инфраструктура", section: "infra", to: "/notifications", label: "Уведомления", icon: Icons.bell, match: (p) => p.startsWith("/notifications"), sidebar: false },
];

export const BOTTOM_NAV: NavItem[] = import.meta.env.VITE_DOCS_PUBLIC === "false" ? [] : [{ href: "/broker-swagger", label: "API-документация", icon: Icons.docs }];

/** The menu item a page belongs to, for its heading's icon and hue. */
export function navItemFor(path: string): NavItem | undefined {
  return MAIN_NAV.find(item => item.match?.(path));
}
