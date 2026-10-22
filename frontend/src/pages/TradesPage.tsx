import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDate, formatDateTime, sideLabel } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { PageHeader } from "../components/PageHeader";
import { useAdminStream } from "../hooks/useAdminStream";
import { DataTable, type TableColumn } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";
import { compactIdentifier, numberValue } from "../lib/table";
import { exportDeals, groupDeals, tradeRows, type Deal } from "../lib/trades";
import type { AdminTrade, TradeCoverage } from "../types/admin";

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
    queryFn: ({ signal }) => api<{ trades: AdminTrade[]; coverage?: TradeCoverage }>("/api/v1/admin/trades", { signal }),
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
    { id: "exec", header: "№ сделки QUIK", value: d => numberValue(d.trades[0].quik_trade_num) || d.label },
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
  return <div className="trades-page space-y-4">
    <PageHeader title="Сделки" subtitle="Сохранённые стороны сделки по номеру QUIK · автоматическая сверка с историей QUIK" />
    {q.data?.coverage && <div className="card text-xs text-muted" role="status">
      В БД: {q.data.coverage.trades} сделок · {q.data.coverage.sides} сторон · Обе стороны сохранены: {q.data.coverage.complete}
      <span> · В таблице: последние {allDeals.length} сделок</span>
      {q.data.coverage.last_success_at && <> · Полнота подтверждена: {formatDateTime(q.data.coverage.last_success_at)}</>}
      {q.data.coverage.sync_stale && <p className="mt-1 text-danger">Актуальная полнота загрузки не подтверждена. Проверьте уведомления и доступность QUIK.</p>}
      {!!q.data.coverage.incomplete && <p className="mt-1 text-danger">Требуют восстановления второй стороны: {q.data.coverage.incomplete}</p>}
      {!!q.data.coverage.report_errors && <p className="mt-1 text-danger">Ошибок исходных отчётов: {q.data.coverage.report_errors}</p>}
      {q.data.coverage.last_sync_error && <p className="mt-1 text-danger">Сверка не завершена: {q.data.coverage.last_sync_error}</p>}
      {!q.data.coverage.last_success_at && <p className="mt-1">Успешной полной сверки с QUIK ещё не было.</p>}
    </div>}
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Сделки" rows={filteredDeals} columns={columns} rowKey={d => d.key} loading={q.isLoading} initialSort={{ id: "created", direction: "desc" }} resetKey={[sideFilter, firmFilter, symbolFilter, adapterFilter].join(":")} onRefresh={() => void q.refetch()} refreshing={q.isFetching} searchPlaceholder="Сделка, ExecID, ордер, клиент…" getSearchText={d => d.label + " " + d.trades.map(t => Object.values(t).join(" ")).join(" ")} exportName="trades" exportRows={exportDeals} emptyTitle="Сделок пока нет" emptyHint="Исполнения появятся после отчёта FIX" filters={<>
      <select className="input w-auto" aria-label="Сторона сделки" value={sideFilter} onChange={e => setSideFilter(e.target.value)}><option value="">Все стороны</option><option value="BUY">Покупка</option><option value="SELL">Продажа</option></select>
      <select className="input w-auto" aria-label="Фирма сделки" value={firmFilter} onChange={e => setFirmFilter(e.target.value)}><option value="">Все фирмы</option>{firmOptions.map(f => <option key={f}>{f}</option>)}</select>
      <select className="input w-auto" aria-label="Инструмент сделки" value={symbolFilter} onChange={e => setSymbolFilter(e.target.value)}><option value="">Все инструменты</option>{symbolOptions.map(f => <option key={f}>{f}</option>)}</select>
      <select className="input w-auto" aria-label="Адаптер сделки" value={adapterFilter} onChange={e => setAdapterFilter(e.target.value)}><option value="">Все адаптеры</option>{adapterOptions.map(f => <option key={f}>{f}</option>)}</select>
      {(sideFilter || firmFilter || symbolFilter || adapterFilter) && <button className="btn-muted" onClick={() => { setSideFilter(""); setFirmFilter(""); setSymbolFilter(""); setAdapterFilter(""); }}>Сбросить</button>}
    </>} summary={rows => [{ label: "Сделок", value: rows.length }, { label: "Сторон", value: rows.reduce((n, d) => n + d.trades.length, 0) }]} renderRow={(deal, dealIdx) => {
                // paired buy/sell share one gray block; alternate shade between deals
                const bg =
                  dealIdx % 2 === 0 ? "bg-slate-50/70" : "bg-white";

                const rows = tradeRows(deal);
                return rows.map((t, legIdx) => {
                  const isLast = legIdx === rows.length - 1;
                  return (
                    <tr
                      key={t.id}
                      data-deal-key={deal.key}
                      data-paired={deal.paired}
                      data-source={t.source}
                      className={`${bg} transition-colors hover:bg-teal-50/50 ${
                        isLast ? "border-b border-slate-200" : "border-b border-slate-100/80"
                      }`}
                    >
                      <td className="px-3 py-2 align-middle">
                        <span className="whitespace-nowrap font-mono text-[11px] text-slate-600">
                          {t.trade_time_known === false && t.execution_date ? formatDate(t.execution_date + "T00:00:00") : formatDateTime(t.created_at)}
                        </span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <Link
                          className="mx-auto block max-w-[160px] truncate font-mono text-[11px] text-accent hover:underline"
                          to={`/trades/${t.detail_id}`}
                          aria-label={`Открыть сделку ${deal.label}`}
                          title={t.trade_match_id || t.exec_id}
                        >
                          {t.quik_trade_num || deal.label}
                        </Link>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        {t.order_id ? <Link
                          className="font-mono text-xs text-accent hover:underline"
                          to={`/orders/${t.order_id}`}
                          title={t.client_order_id || t.order_id}
                        >
                          {compactIdentifier(t.client_order_id || String(t.order_id).slice(0, 8))}
                        </Link> : <span className="font-mono text-xs">{t.quik_order_num ? `№${t.quik_order_num}` : "—"}</span>}
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <div className="text-sm">{t.firm_name || t.quik_firm || "—"}</div>
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
                        <div className="mt-1 text-[10px] text-muted">{t.has_fix_report ? (t.has_quik_record ? "FIX · QUIK" : "FIX") : "История QUIK"}</div>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-sm tabular-nums">{t.last_qty}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-sm tabular-nums">{t.last_px}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs tabular-nums text-slate-600">{t.cum_qty ?? "—"}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs tabular-nums text-slate-600">{t.leaves_qty ?? "—"}</span>
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
