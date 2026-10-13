import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, sideLabel, statusLabel } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { compactIdentifier, dateValue, numberValue } from "../lib/table";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { StatusBadge } from "../components/StatusBadge";
import { DataTable, type TableColumn } from "../components/DataTable";
import { ReconciliationQueue } from "../components/ReconciliationQueue";
import type { AdminOrder, Broker } from "../types/admin";

const format = (v: unknown) => numberValue(v)?.toLocaleString("ru-RU", { maximumFractionDigits: 8 }) ?? "—";
export default function OrdersPage() {
  const { connected } = useAdminStream();
  const [status, setStatus] = useState("");
  const [brokerId, setBrokerId] = useState("");
  const q = useQuery({ queryKey: ["admin-orders"], queryFn: ({ signal }) => api<{ orders: AdminOrder[] }>("/api/v1/admin/orders", { signal }), refetchInterval: liveRefetchInterval(connected, 3000) });
  const brokers = useQuery({ queryKey: ["brokers"], queryFn: ({ signal }) => api<{ brokers: Broker[] }>("/api/v1/admin/brokers", { signal }), staleTime: 60_000 });
  const names = new Map((brokers.data?.brokers ?? []).map(b => [b.id, b.name]));
  const orders = q.data?.orders ?? [];
  const statuses = useMemo(() => Array.from(new Set(orders.map(o => o.status).filter(Boolean))) as string[], [orders]);
  const filtered = orders.filter(o => (!status || o.status === status) && (!brokerId || String(o.broker_id) === brokerId));
  const columns: TableColumn<AdminOrder>[] = [
    { id: "id", header: "Заявка", value: o => o.client_order_id || o.id, cell: o => <Link className="font-mono text-[11px] text-accent whitespace-nowrap" to={`/orders/${o.id}`} title={o.client_order_id || o.id}>{compactIdentifier(o.client_order_id || o.id.slice(0, 8))}</Link> },
    { id: "broker", header: "Брокер", value: o => names.get(o.broker_id ?? 0) || o.broker_id },
    { id: "firm", header: "Фирма", value: o => o.firm_name || o.quik_firm, cell: o => <><div>{o.firm_name || "—"}</div><small className="font-mono text-muted">{o.quik_firm}</small></> },
    { id: "client", header: "Клиент", value: o => o.client_code, className: "font-mono" },
    { id: "symbol", header: "Инструмент", value: o => o.symbol, cell: o => <><strong className="font-medium">{o.symbol}</strong><div className="text-[10px] text-muted">{o.board}</div></> },
    { id: "side", header: "Сторона", value: o => sideLabel(o.side), cell: o => <span className={o.side === "BUY" ? "badge-success" : o.side === "SELL" ? "badge-danger" : "badge-muted"}>{sideLabel(o.side)}</span> },
    { id: "quantity", header: "Кол-во", value: o => numberValue(o.quantity), cell: o => format(o.quantity), numeric: true },
    { id: "price", header: "Цена", value: o => numberValue(o.price), cell: o => format(o.price), numeric: true },
    { id: "status", header: "Статус", value: o => statusLabel(o.status), cell: o => <StatusBadge status={o.status} /> },
    { id: "adapter", header: "Адаптер", value: o => o.adapter_id, className: "text-[10px] font-mono" },
    { id: "created", header: "Создано", value: o => dateValue(o.created_at), exportValue: o => o.created_at, cell: o => formatDateTime(o.created_at), className: "whitespace-nowrap text-[10px]" },
  ];
  return <div className="space-y-4">
    <PageHeader title="Заявки" subtitle="Все брокеры · маршрут, исполнение и состояние каждой заявки" />
    <ReconciliationQueue />
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Заявки" rows={filtered} columns={columns} rowKey={o => o.id} loading={q.isLoading} initialSort={{ id: "created", direction: "desc" }} resetKey={`${status}:${brokerId}`} exportName="orders" searchPlaceholder="Номер, инструмент, фирма, клиент…" getSearchText={o => [o.id, o.client_order_id, o.symbol, o.board, o.firm_name, o.quik_firm, o.client_code, o.adapter_id, o.broker_id, names.get(o.broker_id ?? 0), o.status, statusLabel(o.status), sideLabel(o.side)].join(" ")} onRefresh={() => void q.refetch()} refreshing={q.isFetching} emptyTitle="Заявок нет" emptyHint="Новые заявки появятся после отправки брокером" filters={<>
      <select className="input w-auto" aria-label="Брокер" value={brokerId} onChange={e => setBrokerId(e.target.value)}><option value="">Все брокеры</option>{(brokers.data?.brokers ?? []).map(b => <option key={b.id} value={b.id}>{b.name}</option>)}</select>
      <select className="input w-auto" aria-label="Статус заявки" value={status} onChange={e => setStatus(e.target.value)}><option value="">Все статусы</option>{statuses.map(s => <option key={s} value={s}>{statusLabel(s)}</option>)}</select>
      {(status || brokerId) && <button className="btn-muted" onClick={() => { setStatus(""); setBrokerId(""); }}>Сбросить</button>}
    </>} summary={rows => [{ label: "Заявок", value: rows.length }, { label: "Покупка", value: rows.filter(o => o.side === "BUY").length, tone: "success" }, { label: "Продажа", value: rows.filter(o => o.side === "SELL").length }, { label: "Исполнено", value: rows.filter(o => o.status === "FILLED").length, tone: "success" }, { label: "Отклонено", value: rows.filter(o => o.status === "REJECTED").length, tone: "danger" }]} />
  </div>;
}
