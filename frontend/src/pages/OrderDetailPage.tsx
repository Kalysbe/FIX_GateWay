import { DataTable, type TableColumn } from "../components/DataTable";
import { dateValue, numberValue } from "../lib/table";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, formatTime, sideLabel } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, PageSpinner } from "../components/feedback";
import { StatusBadge } from "../components/StatusBadge";
import type { AdminOrder, AdminTrade, OrderEvent } from "../types/admin";
import { RequestJourney } from "../components/RequestJourney";
import { ReconciliationQueue } from "../components/ReconciliationQueue";

type TraceEvent = {
  id: string;
  timestamp: string;
  type: string;
  subtype: string;
  broker_name?: string;
  client_ip?: string;
  adapter_id?: string;
  message?: string;
};

export default function OrderDetailPage() {
  const { id } = useParams();
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["order", id],
    queryFn: ({ signal }) =>
      api<{ order: AdminOrder; events: OrderEvent[]; trades: AdminTrade[] }>(`/api/v1/admin/orders/${id}`, { signal }),
    refetchInterval: liveRefetchInterval(connected, 2000),
  });
  const trace = useQuery({
    queryKey: ["order-trace", id],
    queryFn: ({ signal }) => api<{ events: TraceEvent[] }>(`/api/v1/admin/orders/${id}/trace`, { signal }),
    refetchInterval: liveRefetchInterval(connected, 3000),
  });

  if (q.isLoading) return <PageSpinner />;
  if (q.error) return <ErrorBanner error={q.error} onRetry={() => q.refetch()} />;
  const o = q.data?.order;
  const events = q.data?.events || [];
  const trades = q.data?.trades || [];

  const tradeColumns: TableColumn<AdminTrade>[] = [
    { id: "created", header: "Время", value: t => dateValue(t.created_at), exportValue: t => t.created_at, cell: t => formatDateTime(t.created_at), className: "text-[10px] whitespace-nowrap" },
    { id: "exec", header: "ExecID", value: t => t.exec_id, className: "font-mono text-[10px]" },
    { id: "qty", header: "Кол-во", value: t => numberValue(t.last_qty), numeric: true },
    { id: "px", header: "Цена", value: t => numberValue(t.last_px), numeric: true },
    { id: "cum", header: "Накоплено", value: t => numberValue(t.cum_qty), numeric: true },
    { id: "leaves", header: "Остаток", value: t => numberValue(t.leaves_qty), numeric: true },
    { id: "adapter", header: "Адаптер", value: t => t.adapter_id, className: "font-mono text-[10px]" },
  ];
  return (
    <div className="space-y-6">
      <PageHeader title={`Заявка ${o?.client_order_id || ""}`} subtitle={<span className="font-mono">{o?.id}</span>} />
      <ReconciliationQueue orderId={id} />
      {trace.error ? <ErrorBanner error={trace.error} onRetry={() => trace.refetch()} /> : trace.isLoading ? <div className="card text-muted">Загрузка пути заявки…</div> : <RequestJourney events={trace.data?.events ?? []} />}
      <div className="grid gap-4 md:grid-cols-2">
        <div className="card space-y-2 text-sm">
          <Row k="Статус" v={<StatusBadge status={o?.status} />} />
          <Row k="Фирма" v={o?.firm_name || o?.quik_firm || "—"} />
          <Row k="Код фирмы" v={o?.quik_firm || "—"} />
          <Row k="Код клиента" v={o?.client_code || "—"} />
          <Row k="ID заявки клиента" v={o?.client_order_id || "—"} />
          <Row k="Инструмент" v={o?.symbol} />
          <Row k="Сторона" v={sideLabel(o?.side)} />
          <Row k="Количество" v={o?.quantity} />
          <Row k="Цена" v={o?.price ?? "—"} />
          <Row k="Исполнено" v={o?.filled_quantity} />
          <Row k="Адаптер" v={o?.adapter_id || "—"} />
          <Row k="FIX ClOrdID" v={o?.fix_cl_ord_id || "—"} />
          <Row k="QUIK OrderID" v={o?.quik_order_id || "—"} />
          <Row k="Request ID" v={o?.request_id || "—"} />
          <Row k="Trace ID" v={o?.trace_id || "—"} />
        </div>
        <div className="card">
          <h2 className="mb-3 font-medium">Жизненный цикл</h2>
          <ol className="space-y-2">
            {events.map((e) => (
              <li key={e.id} className="rounded-lg bg-surface px-3 py-2 text-sm">
                <div className="font-mono text-xs text-slate-500">{formatTime(e.created_at)}</div>
                <div className="font-medium">{e.event_type}</div>
                <div className="text-slate-600">
                  <StatusBadge status={e.from_status} /> → <StatusBadge status={e.to_status} />
                  {e.adapter_id ? ` · ${e.adapter_id}` : ""}
                </div>
                <div className="text-slate-500">{e.message}</div>
                {e.raw_fix ? (
                  <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded border border-slate-200 bg-slate-50 p-2 font-mono text-[11px] leading-relaxed text-emerald-700">
                    {e.raw_fix}
                  </pre>
                ) : null}
              </li>
            ))}
            {events.length === 0 && <EmptyState title="Событий пока нет" />}
          </ol>
        </div>
      </div>

      <DataTable title="Исполнения заявки" rows={trades} columns={tradeColumns} rowKey={t => t.id} exportName="order-executions" initialSort={{ id: "created", direction: "desc" }} emptyTitle="Нет исполнений по этой заявке" summary={rows => [{ label: "Исполнений", value: rows.length }, { label: "Исполнено, ед.", value: rows.reduce((n, t) => n + (numberValue(t.last_qty) ?? 0), 0).toLocaleString("ru-RU", { maximumFractionDigits: 8 }) }]} />

      <div className="card">
        <h2 className="mb-3 font-medium">Полный trace (Gateway → FIX)</h2>
        {(trace.data?.events || []).length === 0 ? (
          <p className="text-sm text-slate-500">События observability появятся по мере прохождения заявки</p>
        ) : (
          <ol className="space-y-2">
            {(trace.data?.events || [])
              .slice()
              .reverse()
              .map((e) => (
                <li key={e.id} className="rounded-lg bg-surface px-3 py-2 text-sm">
                  <div className="font-mono text-xs text-slate-500">{formatTime(e.timestamp)}</div>
                  <div className="font-medium">
                    {e.type} · {e.subtype}
                  </div>
                  <div className="text-slate-600">
                    {e.broker_name ? `${e.broker_name} · ` : ""}
                    {e.client_ip || ""}
                    {e.adapter_id ? ` · ${e.adapter_id}` : ""}
                    {e.message ? ` · ${e.message}` : ""}
                  </div>
                </li>
              ))}
          </ol>
        )}
      </div>
    </div>
  );
}

function Row({ k, v }: { k: string; v: ReactNode }) {
  return (
    <div className="flex justify-between gap-4 border-b border-slate-100 py-1">
      <span className="text-slate-500">{k}</span>
      <span className="text-right font-mono">{v}</span>
    </div>
  );
}
