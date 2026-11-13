import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, orderRowKey, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner, PageSpinner } from "../components/feedback";
import { DataTable } from "../components/DataTable";
import { quoteColumns } from "../lib/quotes";

// Everything QUIK holds on one instrument of today's current trades, each row
// as stored, and its quotes (today's Orders in state "Активна").
type InstrumentDetails = {
  params: DBOrder;
  security: DBOrder | null;
  class: DBOrder | null;
  quotes: DBOrder[];
  quotes_has_more: boolean;
};

function RowCard({ title, row, empty }: { title: string; row: DBOrder | null; empty: string }) {
  return <div className="card overflow-x-auto">
    <div className="mb-2 text-sm font-semibold">{title}</div>
    {row ? <table className="table"><tbody>{Object.entries(row).map(([field, value]) =>
      <tr key={field}><th className="text-left font-mono">{field}</th><td className="whitespace-pre-wrap font-mono">{dbValue(value)}</td></tr>
    )}</tbody></table> : <div className="text-xs text-muted">{empty}</div>}
  </div>;
}

export default function MarketInstrumentPage() {
  const { board = "", symbol = "" } = useParams();
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["admin-market-data", "details", board, symbol],
    queryFn: ({ signal }) => api<InstrumentDetails>(`/api/v1/admin/market-data/${encodeURIComponent(board)}/${encodeURIComponent(symbol)}/details`, { signal }, parseDBJSON),
    refetchInterval: liveRefetchInterval(connected, 2000, 10000),
  });
  const d = q.data;
  const name = d?.security?.SecShortName ?? d?.security?.SecFullName;
  return <div className="space-y-4">
    <PageHeader title={`${board} · ${symbol}`} subtitle={[name == null ? "" : dbValue(name), d?.class?.ClassName == null ? "" : dbValue(d.class.ClassName)].filter(Boolean).join(" · ") || "Рыночные данные инструмента из QUIK"} />
    <Link className="text-sm text-accent" to="/market-data">← Текущие торги</Link>
    {q.isLoading && <PageSpinner />}
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    {d && <>
      <DataTable title={`Котировки${d.quotes_has_more ? " (показаны первые 1000)" : ""}`} rows={d.quotes} columns={quoteColumns()} rowKey={orderRowKey} exportName={`quotes-${board}-${symbol}`} showTotals={false} emptyTitle="Нет активных заявок по инструменту за сегодня" />
      <div className="grid gap-4 xl:grid-cols-2">
        <RowCard title="Рыночные данные · Params (сегодня)" row={d.params} empty="" />
        <div className="space-y-4">
          <RowCard title="Инструмент · Securities" row={d.security} empty="Нет записи в QUIK Securities" />
          <RowCard title="Класс · Classes" row={d.class} empty="Нет записи в QUIK Classes" />
        </div>
      </div>
    </>}
  </div>;
}
