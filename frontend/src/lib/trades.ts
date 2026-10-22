import type { AdminTrade } from "../types/admin";

export type Deal = {
  key: string;
  label: string;
  trades: AdminTrade[];
  paired: boolean;
  createdAt: number;
  symbol: string;
  board: string;
  lastQty: string | number;
  lastPx: string | number;
};

function matchId(trade: AdminTrade): string {
  if (trade.trade_match_id) return trade.trade_match_id;
  const parts = trade.exec_id.trim().match(/^(.+)-([BS])(-[0-9]+-[0-9]+-[^-]+)$/);
  if (!parts || (parts[2] === "B" ? trade.side !== "BUY" : trade.side !== "SELL")) return "";
  return parts[1] + "-*" + parts[3];
}

export function groupDeals(trades: AdminTrade[]): Deal[] {
  const groups = new Map<string, AdminTrade[]>();
  for (const trade of trades) {
    const match = matchId(trade);
    const receivedAt = Date.parse(trade.created_at);
    const day = trade.execution_date || (Number.isFinite(receivedAt) ? new Date(receivedAt).toISOString().slice(0, 10) : "");
    const key = trade.ledger_trade_id || (match && day && trade.symbol && trade.board
      ? JSON.stringify([day, trade.symbol, trade.board, trade.trade_match_id ? "match" : "exec", match])
      : `solo:${trade.id}`);
    const legs = groups.get(key) ?? [];
    legs.push(trade);
    groups.set(key, legs);
  }
  return [...groups].map(([key, legs]) => {
    const sorted = [...legs].sort((a, b) => {
      const side = (a.side === "BUY" ? 0 : 1) - (b.side === "BUY" ? 0 : 1);
      return side || Date.parse(a.created_at) - Date.parse(b.created_at) || a.id.localeCompare(b.id);
    });
    const sample = sorted[0];
    const paired = sorted.some(t => t.side === "BUY") && sorted.some(t => t.side === "SELL");
    return {
      key,
      label: sample.quik_trade_num || sample.trade_match_id || (matchId(sample) || sample.exec_id).replace("-*-", "-"),
      trades: sorted,
      paired,
      createdAt: Math.max(...sorted.map(t => Date.parse(t.created_at))),
      symbol: sample.symbol || "",
      board: sample.board || "",
      lastQty: sample.last_qty,
      lastPx: sample.last_px,
    };
  }).sort((a, b) => b.createdAt - a.createdAt || a.key.localeCompare(b.key));
}

// Render only persisted sides. The server owns trade identity and recovery.
export type TradeRow = Partial<AdminTrade> & {
  id: string;
  detail_id: string;
  source: "execution" | "counterparty";
};

export function tradeRows(deal: Deal): TradeRow[] {
  const rows: TradeRow[] = deal.trades.map(trade => ({ ...trade, detail_id: trade.ledger_trade_id || trade.id, source: "execution" }));
  return rows.sort((a, b) => (a.side === "BUY" ? 0 : 1) - (b.side === "BUY" ? 0 : 1));
}

export function exportDeals(deals: Deal[]) {
  return {
    headers: ["deal_id", "both_sides_recorded", "created_at", "exec_id", "order_id", "client_order_id", "firm_name", "quik_firm", "client_code", "symbol", "board", "side", "last_qty", "last_px", "cum_qty", "leaves_qty", "adapter_id", "source", "quik_order_num", "trade_match_id"],
    rows: deals.flatMap(deal => tradeRows(deal).map(row => [deal.label, deal.paired, row.created_at, row.exec_id, row.order_id, row.client_order_id, row.firm_name, row.quik_firm, row.client_code, row.symbol, row.board, row.side, row.last_qty, row.last_px, row.cum_qty, row.leaves_qty, row.adapter_id, row.has_fix_report ? (row.has_quik_record ? "fix+quik" : "fix") : "quik", row.quik_order_num, row.trade_match_id])),
  };
}
