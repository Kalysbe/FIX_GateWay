import type { CreateOrderInput, LocalOrder, OrderEvent, Side, OrderType, TimeInForce } from "../types";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

function baseUrl(gatewayUrl: string): string {
  return (gatewayUrl || "").replace(/\/$/, "");
}

async function request<T>(
  gatewayUrl: string,
  token: string,
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  headers.set("Authorization", `Bearer ${token.trim()}`);

  const res = await fetch(`${baseUrl(gatewayUrl)}${path}`, { ...init, headers });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(res.status, (body as { error?: string }).error || res.statusText || "request failed");
  }
  return body as T;
}

export type GatewayOrder = {
  id: string;
  broker_id?: number;
  account_id?: string;
  client_order_id: string;
  symbol?: string;
  board?: string;
  side: string;
  order_type: string;
  time_in_force?: string;
  quantity: string | number;
  price?: string | number | null;
  filled_quantity?: string | number;
  avg_price?: string | number | null;
  status: string;
  quik_order_id?: string | null;
  client_code?: string;
  quik_firm?: string;
  reject_reason?: string | null;
  created_at?: string;
  updated_at?: string;
};

export type GatewayAccount = {
  id?: number;
  code?: string;
  name?: string;
  trading_account?: string;
  quik_firm?: string;
};

function str(v: unknown, fallback = ""): string {
  if (v === null || v === undefined) return fallback;
  return String(v);
}

export function mapGatewayOrder(brokerId: string, o: GatewayOrder, events: OrderEvent[] = []): LocalOrder {
  return {
    id: o.id,
    brokerId,
    clientOrderId: o.client_order_id,
    accountId: str(o.account_id),
    clientCode: str(o.client_code),
    quikFirm: str(o.quik_firm),
    symbol: str(o.symbol),
    board: str(o.board),
    side: o.side as Side,
    orderType: o.order_type as OrderType,
    timeInForce: (o.time_in_force || "DAY") as TimeInForce,
    quantity: str(o.quantity),
    price: o.price != null && o.price !== "" ? str(o.price) : undefined,
    status: o.status,
    filledQty: str(o.filled_quantity, "0"),
    avgPrice: o.avg_price != null && o.avg_price !== "" ? str(o.avg_price) : undefined,
    quikOrderId: o.quik_order_id || undefined,
    rejectReason: o.reject_reason || undefined,
    createdAt: o.created_at || new Date().toISOString(),
    updatedAt: o.updated_at || new Date().toISOString(),
    events,
    source: "gateway",
  };
}

/** Validate token against gateway. Returns order count hint. */
export async function probeToken(
  gatewayUrl: string,
  token: string,
): Promise<{ orders: GatewayOrder[]; accounts: GatewayAccount[] }> {
  const [ordersRes, accountsRes] = await Promise.all([
    request<{ orders: GatewayOrder[] }>(gatewayUrl, token, "/api/v1/orders"),
    request<{ accounts: GatewayAccount[] }>(gatewayUrl, token, "/api/v1/accounts").catch(() => ({
      accounts: [] as GatewayAccount[],
    })),
  ]);
  return { orders: ordersRes.orders || [], accounts: accountsRes.accounts || [] };
}

export async function createOrder(
  gatewayUrl: string,
  token: string,
  input: CreateOrderInput,
): Promise<{ order: GatewayOrder; idempotent?: boolean }> {
  const body: Record<string, unknown> = {
    account_id: input.accountId,
    client_code: input.clientCode,
    client_order_id: input.clientOrderId,
    symbol: input.symbol,
    board: input.board,
    side: input.side,
    order_type: input.orderType,
    time_in_force: input.timeInForce || "DAY",
    quantity: input.quantity,
  };
  if (input.orderType === "LIMIT" && input.price != null && input.price !== "") {
    body.price = input.price;
  }
  return request(gatewayUrl, token, "/api/v1/orders", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function listOrders(gatewayUrl: string, token: string): Promise<GatewayOrder[]> {
  const res = await request<{ orders: GatewayOrder[] }>(gatewayUrl, token, "/api/v1/orders");
  return res.orders || [];
}

export async function cancelOrder(gatewayUrl: string, token: string, orderId: string): Promise<GatewayOrder> {
  const res = await request<{ order: GatewayOrder }>(gatewayUrl, token, `/api/v1/orders/${orderId}/cancel`, {
    method: "POST",
  });
  return res.order;
}

export async function getOrder(
  gatewayUrl: string,
  token: string,
  orderId: string,
): Promise<{ order: GatewayOrder; events: Array<{ id: string; event_type?: string; message?: string; created_at?: string }> }> {
  return request(gatewayUrl, token, `/api/v1/orders/${orderId}`);
}
