import type { TableSummary } from "../components/DataTable";

type InstrumentStatus = {
  label: string;
  tone: "badge-success" | "badge-muted" | "badge-warn";
  reported: boolean;
  trading: boolean;
};

// ARQA FIX Protocol, SecurityTradingStatus (326) and TradingSessionID (336).
const STATUS: Record<string, [string, InstrumentStatus["tone"]]> = {
  "2": ["Приостановлены", "badge-warn"],
  "17": ["Идут торги", "badge-success"],
  "18": ["Закрыты", "badge-muted"],
  "100": ["Закрытие", "badge-warn"],
  "101": ["Открытие", "badge-warn"],
  "102": ["Аукцион", "badge-warn"],
  "103": ["Аукцион закрытия", "badge-warn"],
  "104": ["Дискретный аукцион", "badge-warn"],
};
const SESSION_CODES: Record<string, string> = {
  OPEN: "17", CLOSE: "18", CLOSED: "18", SUSPEND: "2", SUSPENDED: "2",
  CLOSING: "100", OPENING: "101", AUCTION: "102",
  CLOSING_AUCTION: "103", DISCRETE_AUCTION: "104",
};

export function instrumentStatus(code?: string | null, session?: string | null): InstrumentStatus {
  const value = code?.trim() || "";
  const sessionValue = session?.trim() || "";
  const key = value || SESSION_CODES[sessionValue.toUpperCase()] || "";
  const definition = STATUS[key];
  if (definition) return { label: definition[0], tone: definition[1], reported: true, trading: key === "17" };
  if (value || sessionValue) return { label: value ? `Статус ${value}` : sessionValue, tone: "badge-muted", reported: true, trading: false };
  return { label: "Нет статуса", tone: "badge-muted", reported: false, trading: false };
}

type MarketRow = {
  board?: string;
  md_req_id?: string;
  trading_status?: string;
  trading_session_id?: string;
  raw_fix?: string;
  bid?: string;
  offer?: string;
  last?: string;
  current_price?: string;
  official_price?: string;
  open?: string;
  high?: string;
  low?: string;
  close?: string;
  wapr?: string;
  volume?: string;
  value?: string;
  bids?: unknown[];
  offers?: unknown[];
};

export function hasReceivedMarketData(row: MarketRow): boolean {
  // Seeded subscription rows have only instrument identifiers and updated_at.
  // A received empty book still counts as a response, even without a price.
  return Boolean(row.md_req_id?.trim() || row.raw_fix?.trim() ||
    instrumentStatus(row.trading_status, row.trading_session_id).reported ||
    row.bids?.length || row.offers?.length ||
    [row.bid, row.offer, row.last, row.current_price, row.official_price, row.open,
      row.high, row.low, row.close, row.wapr, row.volume, row.value].some(v => v != null && String(v).trim() !== ""));
}

export function marketDataSummary(rows: MarketRow[]): TableSummary[] {
  const statuses = rows.map(r => instrumentStatus(r.trading_status, r.trading_session_id));
  const missing = statuses.filter(s => !s.reported).length;
  return [
    { label: "Инструментов", value: rows.length },
    { label: "Данные получены", value: rows.filter(hasReceivedMarketData).length },
    { label: "Торгуются", value: statuses.some(s => s.reported) ? statuses.filter(s => s.trading).length : "—", tone: "success" },
    { label: "Без статуса", value: missing, tone: missing ? "warn" : undefined },
    { label: "Классов", value: new Set(rows.map(r => r.board).filter(Boolean)).size },
  ];
}
