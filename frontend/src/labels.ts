const SIDE: Record<string, string> = {
  BUY: "ПОКУПКА",
  SELL: "ПРОДАЖА",
};

const STATUS: Record<string, string> = {
  FILLED: "ИСПОЛНЕН",
  PARTIALLY_FILLED: "ЧАСТИЧНО ИСПОЛНЕН",
  ACKNOWLEDGED: "ПОДТВЕРЖДЁН",
  REJECTED: "ОТКЛОНЁН",
  FAILED: "ОШИБКА",
  QUEUED: "В ОЧЕРЕДИ",
  SENT: "ОТПРАВЛЕН",
  ROUTING: "МАРШРУТИЗАЦИЯ",
  CANCELLED: "ОТМЕНЁН",
  CANCELED: "ОТМЕНЁН",
  NEW: "НОВЫЙ",
  PENDING: "ОЖИДАНИЕ",
  ONLINE: "ОНЛАЙН",
  CONNECTED: "ПОДКЛЮЧЁН",
  OFFLINE: "ОФЛАЙН",
  DEGRADED: "ДЕГРАДАЦИЯ",
  ACTIVE: "АКТИВЕН",
  BLOCKED: "ЗАБЛОКИРОВАН",
  REVOKED: "ОТОЗВАН",
  EXPIRED: "ИСТЁК",
  DISABLED: "ОТКЛЮЧЁН",
  INACTIVE: "НЕАКТИВЕН",
  UNKNOWN: "НЕИЗВЕСТЕН",
};

const SYSTEM: Record<string, string> = {
  api: "API",
  database: "База данных",
  queue: "Очередь",
};

export function sideLabel(side?: string | null): string {
  if (!side) return "—";
  return SIDE[side.toUpperCase()] || side;
}

export function statusLabel(status?: string | null): string {
  if (!status) return "—";
  return STATUS[status.toUpperCase()] || status;
}

export function systemKeyLabel(key: string): string {
  return SYSTEM[key] || key;
}

export function formatDateTime(value?: string | Date | null): string {
  if (!value) return "—";
  return new Date(value).toLocaleString("ru-RU");
}

export function formatDate(value?: string | Date | null): string {
  if (!value) return "—";
  return new Date(value).toLocaleDateString("ru-RU");
}

export function formatTime(value?: string | Date | null): string {
  if (!value) return "—";
  return new Date(value).toLocaleTimeString("ru-RU");
}

export function formatNumber(n?: number | null): string {
  if (n == null || Number.isNaN(Number(n))) return "—";
  return Number(n).toLocaleString("ru-RU");
}

export function formatMs(n?: number | null): string {
  if (n == null || Number.isNaN(Number(n))) return "—";
  return `${Math.round(Number(n))} мс`;
}

export function formatPct(n?: number | null): string {
  if (n == null || Number.isNaN(Number(n))) return "—";
  return `${Number(n).toFixed(2)}%`;
}

export function formatRelative(value?: string | Date | null): string {
  if (!value) return "—";
  const t = new Date(value).getTime();
  if (Number.isNaN(t)) return "—";
  const sec = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (sec < 10) return "только что";
  if (sec < 60) return `${sec} с назад`;
  const min = Math.round(sec / 60);
  if (min < 60) return `${min} мин назад`;
  const hours = Math.round(min / 60);
  if (hours < 24) return `${hours} ч назад`;
  return formatDateTime(value);
}

const LOG_TYPE: Record<string, string> = {
  API_REQUEST: "API",
  BUSINESS: "Заявки",
  AUTH: "Авторизация",
  FIX: "FIX",
  ADAPTER: "Адаптер",
  SYSTEM: "Система",
  ERROR: "Ошибка",
  AUDIT: "Audit",
};

export function logTypeLabel(t?: string | null): string {
  if (!t) return "—";
  return LOG_TYPE[t] || t;
}
