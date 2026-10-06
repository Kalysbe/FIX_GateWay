export type Side = "BUY" | "SELL";
export type OrderType = "MARKET" | "LIMIT";
export type TimeInForce = "DAY" | "IOC" | "FOK" | "GTC";

export type OrderStatus =
  | "QUEUED"
  | "SENT"
  | "ROUTING"
  | "ACKNOWLEDGED"
  | "PARTIALLY_FILLED"
  | "FILLED"
  | "CANCEL_PENDING"
  | "CANCELLED"
  | "REJECTED"
  | "EXPIRED"
  | "FAILED"
  | string;

export type ConnectionStatus = "DISCONNECTED" | "CONNECTING" | "CONNECTED" | "ERROR";

export interface BrokerProfile {
  id: string;
  name: string;
  code: string;
  /** Full API token (sk_live_… / sk_test_…). Stored only in sessionStorage. */
  apiToken: string;
  accountId: string;
  clientCode: string;
  /** Informational — real firm comes from token on gateway. */
  quikFirm: string;
  color: string;
  connected: boolean;
  connectionStatus: ConnectionStatus;
  connectedAt?: string;
  lastError?: string;
  gatewayBrokerId?: number;
  createdAt: string;
}

export interface LocalOrder {
  id: string;
  brokerId: string;
  clientOrderId: string;
  accountId: string;
  clientCode: string;
  quikFirm: string;
  symbol: string;
  board: string;
  side: Side;
  orderType: OrderType;
  timeInForce: TimeInForce;
  quantity: string;
  price?: string;
  status: OrderStatus;
  filledQty: string;
  avgPrice?: string;
  quikOrderId?: string;
  rejectReason?: string;
  createdAt: string;
  updatedAt: string;
  events: OrderEvent[];
  source: "gateway";
}

export interface OrderEvent {
  id: string;
  at: string;
  type: string;
  message: string;
}

export interface DeskSettings {
  /** Empty = same-origin / Vite proxy to gateway */
  gatewayUrl: string;
  pollIntervalMs: number;
}

export interface AppState {
  brokers: BrokerProfile[];
  orders: LocalOrder[];
  activeBrokerId: string | null;
  settings: DeskSettings;
  activityLog: ActivityEntry[];
}

export interface ActivityEntry {
  id: string;
  at: string;
  brokerId?: string;
  level: "info" | "ok" | "warn" | "error";
  message: string;
}

export interface CreateOrderInput {
  clientOrderId?: string;
  accountId?: string;
  clientCode?: string;
  symbol: string;
  board: string;
  side: Side;
  orderType: OrderType;
  timeInForce: TimeInForce;
  quantity: string;
  price?: string;
}

export const DEFAULT_SETTINGS: DeskSettings = {
  gatewayUrl: "",
  pollIntervalMs: 2000,
};

export const BROKER_COLORS = [
  "#5eead4",
  "#60a5fa",
  "#fbbf24",
  "#c084fc",
  "#34d399",
  "#fb7185",
  "#a3e635",
  "#38bdf8",
];
