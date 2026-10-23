export type SessionUser = {
  id?: string;
  email?: string;
  role?: string;
  full_name?: string;
};

export type Broker = {
  id: number;
  code: string;
  name: string;
  description?: string;
  status: string;
  created_at: string;
  updated_at?: string;
  connected?: boolean;
  last_activity?: string;
  last_ip?: string;
  ip_whitelist_enabled?: boolean;
};

export type BrokerAccount = {
  id: number;
  broker_id: number;
  code: string;
  name?: string;
  quik_firm?: string;
  trading_account: string;
  status: string;
  created_at?: string;
  updated_at?: string;
};

export type APIToken = {
  id: string;
  broker_id?: number;
  broker_name?: string;
  broker_code?: string;
  name: string;
  description?: string;
  status: string;
  token_prefix: string;
  quik_firm?: string;
  permissions: string[];
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  last_used_ip?: string;
  revoked_at?: string;
  rotated_from?: string;
};

export type TokenOverview = {
  token: APIToken;
  broker: Broker & { rate_limit_rps: number; rate_limit_orders_per_sec: number };
  ip_whitelist: IPWhitelistEntry[];
};

export type IPWhitelistEntry = {
  id: number;
  ip_cidr: string;
  note?: string;
};

export type Adapter = {
  enabled?: boolean;
  id: string;
  name: string;
  type?: string;
  host?: string;
  port?: number;
  status: string;
  weight?: number;
  active_orders?: number;
  processed_orders?: number;
  error_count?: number;
};

export type AdminOrder = {
  id: string;
  source?: string;
  quik_order_num?: string;
  gateway_order_id?: string;
  quik_state?: string;
  account_id?: string;
  quik_type?: string;
  expiry?: string;
  balance?: string | number | null;
  trade_date?: string;
  broker_id?: number;
  firm_name?: string;
  quik_firm?: string;
  client_code?: string;
  client_order_id?: string;
  symbol?: string;
  board?: string;
  side?: string;
  quantity?: string | number;
  price?: string | number | null;
  filled_quantity?: string | number;
  status?: string;
  adapter_id?: string | null;
  created_at?: string;
  fix_cl_ord_id?: string;
  quik_order_id?: string;
  request_id?: string;
  trace_id?: string;
};

export type OrderEvent = {
  id?: string;
  created_at?: string;
  event_type?: string;
  from_status?: string;
  to_status?: string;
  adapter_id?: string;
  message?: string;
  raw_fix?: string;
};

export type AdminTrade = {
  id: string;
  order_id?: string | null;
  exec_id: string;
  last_qty: string | number;
  last_px: string | number;
  cum_qty?: string | number | null;
  leaves_qty?: string | number | null;
  adapter_id?: string | null;
  created_at: string;
  execution_date?: string;
  trade_match_id?: string;
  contra_broker?: string;
  contra_trader?: string;
  client_order_id?: string;
  symbol?: string;
  board?: string;
  side?: string;
  client_code?: string;
  quik_firm?: string;
  firm_name?: string;
  fix_cl_ord_id?: string | null;
  quik_order_id?: string | null;
  quik_order_num?: string;
  quik_trade_num?: string;
  ledger_trade_id?: string;
  execution_id?: string | null;
  has_fix_report?: boolean;
  has_quik_record?: boolean;
  received_at?: string;
  trade_time_known?: boolean;
  account?: string;
};

export type TradeCoverage = {
  trades: number; sides: number; complete: number; incomplete: number; report_errors: number;
  last_sync_at?: string; last_sync_records?: number; last_sync_error?: string; sync_running?: boolean;
  last_success_at?: string; sync_stale?: boolean;
};

export type Dashboard = {
  generated_at?: string;
  stale?: boolean;
  system: { api: string; database: string; queue: string };
  adapters: Adapter[];
  /** null while the QUIK Orders source is unavailable; see orders_source_error. */
  stats: { total: number; filled: number; rejected: number; active: number } | null;
  orders_source_error?: string;
  brokers: number;
  active_accounts: number;
  adapters_available: number;
  observability?: {
    requests_today: number;
    orders_today: number;
    successful: number;
    failed: number;
    success_rate: number;
    avg_latency_ms: number;
    p95_latency_ms: number;
    p99_latency_ms: number;
    errors_today: number;
    critical_today: number;
    auth_failures_today: number;
    active_brokers: number;
    connected_now?: number;
  };
  connected_brokers?: Array<{
    broker_id: number;
    broker_code: string;
    broker_name: string;
    last_activity: string;
    last_ip?: string;
    requests: number;
  }>;
  top_brokers?: Array<{
    broker_id: number;
    broker_name: string;
    requests: number;
    orders: number;
    errors: number;
    success_rate: number;
    avg_latency_ms: number;
    last_activity?: string;
  }>;
  top_errors?: Array<{
    error: string;
    count: number;
    last_occurrence: string;
    affected_brokers: number;
  }>;
};

export type Notification = {
  id: string;
  severity: string;
  event_type?: string;
  subtype?: string;
  error_code?: string;
  broker_id?: number | null;
  broker_name?: string;
  title: string;
  message?: string;
  count: number;
  last_seen_at: string;
  read_at?: string | null;
};

export type AuditRow = {
  id: number;
  action: string;
  result?: string | null;
  error?: string | null;
  ip?: string | null;
  broker_id?: number | null;
  created_at: string;
};

export type ObsLog = {
  id: string;
  timestamp: string;
  type: string;
  subtype: string;
  severity: string;
  error_code?: string;
  message?: string;
  client_ip?: string;
  endpoint?: string;
};

export const ALL_PERMS = [
  "orders:create",
  "orders:read",
  "orders:cancel",
  "orders:replace",
  "trades:read",
  "positions:read",
  "positions:write",
] as const;

export const READ_ONLY_PERMS = ["orders:read", "trades:read", "positions:read"];
