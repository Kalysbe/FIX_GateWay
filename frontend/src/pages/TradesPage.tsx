import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, sideLabel } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { PageHeader } from "../components/PageHeader";
import { useAdminStream } from "../hooks/useAdminStream";
import { DataTable, type TableColumn } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";
import { compactIdentifier, numberValue } from "../lib/table";

type Trade = {
  id: string;
  order_id: string;
  exec_id: string;
  last_qty: string | number;
  last_px: string | number;
  cum_qty: string | number;
  leaves_qty: string | number;
  adapter_id?: string | null;
  created_at: string;
  client_order_id?: string;
  symbol?: string;
  board?: string;
  side?: string;
  client_code?: string;
  quik_firm?: string;
  firm_name?: string;
  fix_cl_ord_id?: string | null;
  quik_order_id?: string | null;
};

type Deal = {
  key: string;
  label: string;
  trades: Trade[];
  paired: boolean;
  createdAt: number;
  symbol: string;
  board: string;
  lastQty: string | number;
  lastPx: string | number;
};

/** KASE/QUIK: LISTEDSTOCKK-4g-B-1-1-N ↔ LISTEDSTOCKK-4g-S-1-1-N */
function matchKeyFromExecId(execId: string): string {
  const id = String(execId || "").trim();
  if (!id) return "";
  const normalized = id.replace(/-([BS])-/i, "-*-");
  return normalized || id;
}

function dealLabelFromMatchKey(key: string): string {
  return key.replace("-*-", "-");
}

function SideBadge({ side }: { side?: string }) {
  const buy = (side || "").toUpperCase() === "BUY";
  return (
    <span
      className={`inline-flex rounded-md px-2 py-0.5 text-xs font-semibold ${
        buy ? "bg-emerald-50 text-emerald-700" : "bg-rose-50 text-rose-700"
      }`}
    >
      {sideLabel(side)}
    </span>
  );
}

function groupDeals(trades: Trade[]): Deal[] {
  const map = new Map<string, Trade[]>();
  for (const t of trades) {
    const key = matchKeyFromExecId(t.exec_id) || `solo:${t.id}`;
    const list = map.get(key) || [];
    list.push(t);
    map.set(key, list);
  }

  const deals: Deal[] = [];
  for (const [key, legs] of map) {
    const sorted = [...legs].sort((a, b) => {
      const sa = (a.side || "").toUpperCase() === "BUY" ? 0 : 1;
      const sb = (b.side || "").toUpperCase() === "BUY" ? 0 : 1;
      if (sa !== sb) return sa - sb;
      return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
    });
    const sides = new Set(sorted.map((t) => (t.side || "").toUpperCase()));
    const paired = sides.has("BUY") && sides.has("SELL") && sorted.length >= 2;
    const sample = sorted[0];
    deals.push({
      key,
      label: dealLabelFromMatchKey(key.startsWith("solo:") ? sample.exec_id : key),
      trades: sorted,
      paired,
      createdAt: Math.max(...sorted.map((t) => new Date(t.created_at).getTime())),
      symbol: sample.symbol || "",
      board: sample.board || "",
      lastQty: sample.last_qty,
      lastPx: sample.last_px,
    });
  }

  return deals.sort((a, b) => b.createdAt - a.createdAt);
}

function dealPassesFilters(
  deal: Deal,
  filters: { side: string; firm: string; symbol: string; adapter: string },
): boolean {
  return deal.trades.some((t) => {
    if (filters.side && t.side !== filters.side) return false;
    if (filters.firm) {
      const label = t.firm_name || t.quik_firm || "";
      if (label !== filters.firm) return false;
    }
    if (filters.symbol && t.symbol !== filters.symbol) return false;
    if (filters.adapter && t.adapter_id !== filters.adapter) return false;
    return true;
  });
}

export default function TradesPage() {
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["admin-trades"],
    queryFn: ({ signal }) => api<{ trades: Trade[] }>("/api/v1/admin/trades", { signal }),
    refetchInterval: liveRefetchInterval(connected, 5000),
  });

  const data = q.data?.trades || [];
  const [sideFilter, setSideFilter] = useState("");
  const [firmFilter, setFirmFilter] = useState("");
  const [symbolFilter, setSymbolFilter] = useState("");
  const [adapterFilter, setAdapterFilter] = useState("");

  const allDeals = useMemo(() => groupDeals(data), [data]);

  const firmOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      const label = t.firm_name || t.quik_firm;
      if (label) set.add(label);
    });
    return Array.from(set).sort((a, b) => a.localeCompare(b, "ru"));
  }, [data]);

  const symbolOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      if (t.symbol) set.add(t.symbol);
    });
    return Array.from(set).sort();
  }, [data]);

  const adapterOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      if (t.adapter_id) set.add(t.adapter_id);
    });
    return Array.from(set).sort();
  }, [data]);

  const filteredDeals = allDeals.filter(d => dealPassesFilters(d, { side: sideFilter, firm: firmFilter, symbol: symbolFilter, adapter: adapterFilter }));
  const columns: TableColumn<Deal>[] = [
    { id: "created", header: "Время", value: d => d.createdAt },
    { id: "exec", header: "ExecID", value: d => d.label },
    { id: "order", header: "Ордер", value: d => d.trades[0].client_order_id || d.trades[0].order_id },
    { id: "firm", header: "Фирма", value: d => d.trades[0].firm_name || d.trades[0].quik_firm },
    { id: "client", header: "Клиент", value: d => d.trades[0].client_code },
    { id: "symbol", header: "Инструмент", value: d => d.symbol },
    { id: "side", header: "Сторона", value: d => d.paired ? "BUY SELL" : d.trades[0].side },
    { id: "quantity", header: "Кол-во", value: d => numberValue(d.lastQty), numeric: true },
    { id: "price", header: "Цена", value: d => numberValue(d.lastPx), numeric: true },
    { id: "cumulative", header: "Накоплено", value: d => numberValue(d.trades[0].cum_qty), numeric: true },
    { id: "leaves", header: "Остаток", value: d => numberValue(d.trades[0].leaves_qty), numeric: true },
    { id: "adapter", header: "Адаптер", value: d => d.trades[0].adapter_id },
  ];
  return <div className="space-y-4">
    <PageHeader title="Сделки" subtitle="Исполнения заявок · парные покупка и продажа остаются в одном блоке" />
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Сделки" rows={filteredDeals} columns={columns} rowKey={d => d.key} loading={q.isLoading} initialSort={{ id: "created", direction: "desc" }} resetKey={[sideFilter, firmFilter, symbolFilter, adapterFilter].join(":")} onRefresh={() => void q.refetch()} refreshing={q.isFetching} searchPlaceholder="Сделка, ExecID, ордер, клиент…" getSearchText={d => d.label + " " + d.trades.map(t => Object.values(t).join(" ")).join(" ")} exportName="trades" exportRows={deals => ({ headers: ["deal_id", "paired", "created_at", "exec_id", "order_id", "client_order_id", "firm_name", "quik_firm", "client_code", "symbol", "board", "side", "last_qty", "last_px", "cum_qty", "leaves_qty", "adapter_id"], rows: deals.flatMap(d => d.trades.map(t => [d.label, d.paired, t.created_at, t.exec_id, t.order_id, t.client_order_id, t.firm_name, t.quik_firm, t.client_code, t.symbol, t.board, t.side, t.last_qty, t.last_px, t.cum_qty, t.leaves_qty, t.adapter_id])) })} emptyTitle="Сделок пока нет" emptyHint="Исполнения появятся после отчёта FIX" filters={<>
      <select className="input w-auto" aria-label="Сторона сделки" value={sideFilter} onChange={e => setSideFilter(e.target.value)}><option value="">Все стороны</option><option value="BUY">Покупка</option><option value="SELL">Продажа</option></select>
      <select className="input w-auto" aria-label="Фирма сделки" value={firmFilter} onChange={e => setFirmFilter(e.target.value)}><option value="">Все фирмы</option>{firmOptions.map(f => <option key={f}>{f}</option>)}</select>
      <select className="input w-auto" aria-label="Инструмент сделки" value={symbolFilter} onChange={e => setSymbolFilter(e.target.value)}><option value="">Все инструменты</option>{symbolOptions.map(f => <option key={f}>{f}</option>)}</select>
      <select className="input w-auto" aria-label="Адаптер сделки" value={adapterFilter} onChange={e => setAdapterFilter(e.target.value)}><option value="">Все адаптеры</option>{adapterOptions.map(f => <option key={f}>{f}</option>)}</select>
      {(sideFilter || firmFilter || symbolFilter || adapterFilter) && <button className="btn-muted" onClick={() => { setSideFilter(""); setFirmFilter(""); setSymbolFilter(""); setAdapterFilter(""); }}>Сбросить</button>}
    </>} summary={rows => [{ label: "Сделок", value: rows.length }, { label: "Исполнений", value: rows.reduce((n, d) => n + d.trades.length, 0) }, { label: "Парных", value: rows.filter(d => d.paired).length, tone: "success" }]} renderRow={(deal, dealIdx) => {
                // paired buy/sell share one gray block; alternate shade between deals
                const bg =
                  dealIdx % 2 === 0 ? "bg-slate-50/70" : "bg-white";

                return deal.trades.map((t, legIdx) => {
                  const isLast = legIdx === deal.trades.length - 1;
                  return (
                    <tr
                      key={t.id}
                      className={`${bg} transition-colors hover:bg-teal-50/50 ${
                        isLast ? "border-b border-slate-200" : "border-b border-slate-100/80"
                      }`}
                    >
                      <td className="px-3 py-2 align-middle">
                        <span className="whitespace-nowrap font-mono text-[11px] text-slate-600">
                          {formatDateTime(t.created_at)}
                        </span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span
                          className="block max-w-[160px] truncate font-mono text-[11px]"
                          title={t.exec_id}
                        >
                          {t.exec_id || "—"}
                        </span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <Link
                          className="font-mono text-xs text-accent hover:underline"
                          to={`/orders/${t.order_id}`}
                          title={t.client_order_id || t.order_id}
                        >
                          {compactIdentifier(t.client_order_id || String(t.order_id).slice(0, 8))}
                        </Link>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <div className="text-sm">{t.firm_name || "—"}</div>
                        {t.quik_firm ? (
                          <div className="font-mono text-[11px] text-slate-500">{t.quik_firm}</div>
                        ) : null}
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs">{t.client_code || "—"}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <div className="font-mono text-xs">{t.symbol || "—"}</div>
                        {t.board ? <div className="text-[11px] text-slate-500">{t.board}</div> : null}
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <SideBadge side={t.side} />
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-sm tabular-nums">{t.last_qty}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-sm tabular-nums">{t.last_px}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs tabular-nums text-slate-600">{t.cum_qty}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs tabular-nums text-slate-600">{t.leaves_qty}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="rounded bg-white/70 px-1.5 py-0.5 font-mono text-[11px] text-slate-600">
                          {t.adapter_id || "—"}
                        </span>
                      </td>
                    </tr>
                  );
                });
    }} />
  </div>;
}
