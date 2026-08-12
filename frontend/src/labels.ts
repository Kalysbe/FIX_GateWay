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
