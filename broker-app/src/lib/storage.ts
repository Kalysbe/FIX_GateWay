import {
  AppState,
  BROKER_COLORS,
  BrokerProfile,
  DEFAULT_SETTINGS,
  DeskSettings,
  LocalOrder,
} from "../types";

const STORAGE_KEY = "broker-desk-v2";
const TOKEN_SESSION_KEY = "broker-desk-tokens-v2";

function uid(prefix = "id"): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return `${prefix}_${crypto.randomUUID()}`;
  }
  return `${prefix}_${Math.random().toString(36).slice(2, 10)}_${Date.now().toString(36)}`;
}

export function createId(prefix?: string): string {
  return uid(prefix);
}

export function nowIso(): string {
  return new Date().toISOString();
}

export function emptyState(): AppState {
  return {
    brokers: [],
    orders: [],
    activeBrokerId: null,
    settings: { ...DEFAULT_SETTINGS },
    activityLog: [],
  };
}

export function loadState(): AppState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return emptyState();
    const parsed = JSON.parse(raw) as AppState;
    const tokens = loadSessionTokens();
    return {
      ...emptyState(),
      ...parsed,
      settings: { ...DEFAULT_SETTINGS, ...(parsed.settings || {}) },
      brokers: (parsed.brokers || []).map((b) => ({
        ...b,
        apiToken: tokens[b.id] || b.apiToken || "",
        connected: false,
        connectionStatus: "DISCONNECTED" as const,
        lastError: undefined,
      })),
      orders: parsed.orders || [],
      activityLog: (parsed.activityLog || []).slice(0, 200),
    };
  } catch {
    return emptyState();
  }
}

export function saveState(state: AppState): void {
  saveSessionTokens(state.brokers);
  const trimmed: AppState = {
    ...state,
    brokers: state.brokers.map((b) => ({
      ...b,
      apiToken: "",
      connected: false,
      connectionStatus: "DISCONNECTED",
    })),
    activityLog: state.activityLog.slice(0, 200),
    orders: state.orders.slice(0, 1000),
  };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmed));
}

function loadSessionTokens(): Record<string, string> {
  try {
    const raw = sessionStorage.getItem(TOKEN_SESSION_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, string>;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function saveSessionTokens(brokers: BrokerProfile[]): void {
  const map: Record<string, string> = {};
  for (const b of brokers) {
    if (b.apiToken.trim()) map[b.id] = b.apiToken;
  }
  sessionStorage.setItem(TOKEN_SESSION_KEY, JSON.stringify(map));
}

export function exportSafeState(state: AppState): string {
  const safe: AppState = {
    ...state,
    brokers: state.brokers.map((b) => ({ ...b, apiToken: "", connected: false, connectionStatus: "DISCONNECTED" })),
  };
  return JSON.stringify(safe, null, 2);
}

export function makeBroker(partial?: Partial<BrokerProfile>, index = 0): BrokerProfile {
  const n = index + 1;
  const base: BrokerProfile = {
    id: createId("brk"),
    name: `Broker ${n}`,
    code: `BRK${String(n).padStart(2, "0")}`,
    apiToken: "",
    accountId: "",
    clientCode: "",
    quikFirm: "",
    color: BROKER_COLORS[index % BROKER_COLORS.length],
    connected: false,
    connectionStatus: "DISCONNECTED",
    createdAt: nowIso(),
  };
  return { ...base, ...partial, id: partial?.id || base.id, createdAt: partial?.createdAt || base.createdAt };
}

export function updateOrderInList(orders: LocalOrder[], order: LocalOrder): LocalOrder[] {
  const idx = orders.findIndex((o) => o.id === order.id);
  if (idx === -1) return [order, ...orders];
  const next = orders.slice();
  next[idx] = order;
  return next;
}

export function mergeBrokerOrders(
  existing: LocalOrder[],
  brokerId: string,
  incoming: LocalOrder[],
): LocalOrder[] {
  const others = existing.filter((o) => o.brokerId !== brokerId);
  const byId = new Map(incoming.map((o) => [o.id, o]));
  // keep any local-only events if same id
  for (const o of existing) {
    if (o.brokerId !== brokerId) continue;
    const mapped = byId.get(o.id);
    if (mapped && o.events?.length && !mapped.events.length) {
      byId.set(o.id, { ...mapped, events: o.events });
    }
  }
  return [...Array.from(byId.values()), ...others].sort(
    (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
  );
}

export function mergeSettings(current: DeskSettings, patch: Partial<DeskSettings>): DeskSettings {
  return { ...current, ...patch };
}

export function maskToken(token: string): string {
  const t = token.trim();
  if (t.length < 12) return t ? "••••••••" : "";
  return `${t.slice(0, 8)}…${t.slice(-4)}`;
}
