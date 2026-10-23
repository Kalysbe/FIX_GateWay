import { useMemo, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbSortValue, dbValue, nextOrderOffset, ORDER_PAGE_SIZE, orderKey, orderRowKey, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable, type TableColumn } from "../components/DataTable";
import type { Broker } from "../types/admin";

// One server page at a time: the Orders table can be large, and every
// order.updated refetches the visible page only, not the whole table.
export default function OrdersPage() {
  const { connected } = useAdminStream();
  const [state, setState] = useState("");
  const [brokerId, setBrokerId] = useState("");
  const [offsets, setOffsets] = useState<number[]>([0]);
  const offset = offsets[offsets.length - 1];
  const q = useQuery({
    queryKey: ["admin-orders", brokerId, offset],
    queryFn: async ({ signal }) => {
      const { data, headers } = await apiWithHeaders<DBOrder[]>(`/api/v1/admin/orders?limit=${ORDER_PAGE_SIZE}&offset=${offset}${brokerId ? `&broker_id=${encodeURIComponent(brokerId)}` : ""}`, { signal }, parseDBJSON);
      return { rows: data ?? [], next: nextOrderOffset(headers) };
    },
    placeholderData: keepPreviousData,
    refetchInterval: liveRefetchInterval(connected, 3000),
  });
  const brokers = useQuery({ queryKey: ["brokers"], queryFn: ({ signal }) => api<{ brokers: Broker[] }>("/api/v1/admin/brokers", { signal }), staleTime: 60_000 });
  const orders = q.data?.rows ?? [];
  const states = useMemo(() => Array.from(new Set(orders.map(o => String(o.State ?? "")).filter(Boolean))), [orders]);
  const fields = useMemo(() => Array.from(new Set(orders.flatMap(o => Object.keys(o)))), [orders]);
  const filtered = orders.filter(o => !state || o.State === state);
  const columns: TableColumn<DBOrder>[] = fields.map(field => ({
    id: field, header: field, value: o => dbSortValue(o[field]), exportValue: o => dbValue(o[field]),
    cell: o => field === "OrderNum" && orderKey(o)
      ? <Link className="font-mono text-[11px] text-accent whitespace-nowrap" to={`/orders/${encodeURIComponent(orderKey(o))}`}>{dbValue(o[field])}</Link>
      : <span className="whitespace-pre-wrap">{dbValue(o[field])}</span>,
  }));
  const changeBroker = (value: string) => { setBrokerId(value); setOffsets([0]); };
  const first = offset + 1, last = offset + orders.length;
  return <div className="raw-orders space-y-4">
    <PageHeader title="Заявки" />
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Заявки" rows={q.error ? [] : filtered} columns={columns} rowKey={orderRowKey} getSearchText={o => fields.map(f => dbValue(o[f])).join(" ")} loading={q.isLoading} resetKey={`${state}:${brokerId}:${offset}`} exportName="orders" showTotals={false} onRefresh={() => void q.refetch()} refreshing={q.isFetching} searchPlaceholder="Поиск по загруженной странице…" emptyTitle={q.error ? "Заявки временно недоступны" : "Заявок нет"} filters={<>
      <select className="input w-auto" aria-label="Брокер" value={brokerId} onChange={e => changeBroker(e.target.value)}><option value="">Все брокеры</option>{(brokers.data?.brokers ?? []).map(b => <option key={b.id} value={b.id}>{b.name}</option>)}</select>
      <select className="input w-auto" aria-label="Состояние (State) на странице" value={state} onChange={e => setState(e.target.value)}><option value="">Все состояния</option>{states.map(s => <option key={s} value={s}>{s}</option>)}</select>
      {(state || brokerId) && <button className="btn-muted" onClick={() => { setState(""); changeBroker(""); }}>Сбросить</button>}
    </>} />
    <div className="flex items-center justify-end gap-2 text-xs text-muted">
      <span aria-live="polite">Строки БД {orders.length ? `${first}–${last}` : "—"}</span>
      <button type="button" className="btn-muted" disabled={offsets.length < 2 || q.isFetching} onClick={() => setOffsets(o => o.slice(0, -1))}>← Предыдущие {ORDER_PAGE_SIZE}</button>
      <button type="button" className="btn-muted" disabled={q.data?.next == null || q.isFetching} onClick={() => { const next = q.data?.next; if (next != null) setOffsets(o => [...o, next]); }}>Следующие {ORDER_PAGE_SIZE} →</button>
    </div>
  </div>;
}
