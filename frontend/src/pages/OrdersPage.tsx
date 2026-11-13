import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, nextOrderOffset, ORDER_PAGE_SIZE, orderKey, orderRowKey, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { instrumentPath } from "../lib/quotes";
import { useUrlState } from "../lib/urlState";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable } from "../components/DataTable";
import { RowDrawer } from "../components/RowDrawer";
import { rawSearchText } from "../components/rawColumns";
import { quikColumns, type QuikColumn } from "../components/quikColumns";
import type { Broker } from "../types/admin";

const orderLink = (o: DBOrder) => orderKey(o) ? `/orders/${encodeURIComponent(orderKey(o))}` : undefined;

// What a bond order needs at a glance; every other Orders column is in the
// column menu.
const ORDER_COLUMNS: QuikColumn[] = [
  { id: "num", header: "Заявка", field: "OrderNum", link: orderLink },
  // All rows are today's, so the time alone.
  { id: "time", header: "Время", field: ["OrderDateTime", "TradeDate"], kind: "time" },
  { id: "instrument", header: "Инструмент", field: "SecCode", sub: "ClassCode", link: o => instrumentPath(o.ClassCode, o.SecCode) },
  { id: "side", header: "Операция", field: "Operation", kind: "side" },
  // Every row is "Активна"; the column stays in the menu.
  { id: "state", header: "Состояние", field: "State", kind: "state", hidden: true },
  { id: "price", header: "Цена", field: "Price", kind: "number", frac: 2, trend: true },
  { id: "yield", header: "Дох-ть, %", field: "Yield", kind: "number", frac: 2 },
  { id: "qty", header: "Кол-во", field: "Qty", kind: "number" },
  { id: "balance", header: "Остаток", field: "Balance", kind: "number", trend: true },
  { id: "value", header: "Объём", field: "Value", kind: "number", frac: 2 },
  { id: "accrued", header: "НКД", field: "Accruedint", kind: "number", frac: 2 },
  { id: "currency", header: "Валюта", field: ["PriceCurrency", "SettleCurrency"] },
  { id: "account", header: "Счёт / клиент", field: "Account", sub: "ClientCode" },
  { id: "firm", header: "Фирма", field: "FirmId", sub: "FirmName" },
  { id: "settle", header: "Расчёты", field: "SettleDate", kind: "date" },
  { id: "reject", header: "Причина отказа", field: "RejectReason" },
];

// Today's active rows of QUIK Orders, one server page at a time; every
// order.updated refetches the visible page only.
export default function OrdersPage() {
  const { connected } = useAdminStream();
  const [brokerId, setBrokerId] = useUrlState("broker");
  const [offsets, setOffsets] = useState<number[]>([0]);
  const [open, setOpen] = useState<DBOrder | null>(null);
  const [visibleOrder, setVisibleOrder] = useState<DBOrder[]>([]);
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
  const columns = useMemo(() => quikColumns(orders, ORDER_COLUMNS), [orders]);
  const changeBroker = (value: string) => { setBrokerId(value); setOffsets([0]); };
  const serverPage = { start: offset + 1, end: offset + orders.length, size: ORDER_PAGE_SIZE, hasPrevious: offsets.length > 1, hasNext: q.data?.next != null, busy: q.isFetching,
    onPrevious: () => setOffsets(o => o.slice(0, -1)), onNext: () => { const next = q.data?.next; if (next != null) setOffsets(o => [...o, next]); } };
  // The drawer steps through the rows in the order the table shows them, with their latest values.
  const latest = new Map(orders.map(o => [orderRowKey(o), o]));
  const drawerRows = visibleOrder.map(o => latest.get(orderRowKey(o))).filter((o): o is DBOrder => !!o);
  return <div className="raw-orders space-y-4">
    <PageHeader title="Заявки" subtitle="Активные заявки QUIK за сегодняшний торговый день · строка открывается по клику" />
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Заявки" rows={q.error ? [] : orders} columns={columns} rowKey={orderRowKey} getSearchText={rawSearchText(orders)} loading={q.isLoading} resetKey={`${brokerId}:${offset}`} exportName="orders" showTotals={false} onRefresh={() => void q.refetch()} refreshing={q.isFetching} searchPlaceholder="Поиск по загруженной странице…" emptyTitle={q.error ? "Заявки временно недоступны" : "Активных заявок за сегодня нет"}
      live urlState selection={{}} activeRowKey={open ? orderRowKey(open) : null} onRowClick={(row, sorted) => { setVisibleOrder(sorted); setOpen(row); }}
      filters={<>
        <select className="input w-auto" aria-label="Брокер" value={brokerId} onChange={e => changeBroker(e.target.value)}><option value="">Все брокеры</option>{(brokers.data?.brokers ?? []).map(b => <option key={b.id} value={b.id}>{b.name}</option>)}</select>
        {brokerId && <button className="btn-muted" onClick={() => changeBroker("")}>Сбросить</button>}
      </>} serverPage={serverPage} />
    <RowDrawer row={open} rows={drawerRows} rowKey={orderRowKey} onChange={setOpen}
      title={o => `Заявка ${dbValue(o.OrderNum)}`}
      subtitle={o => [o.SecCode, o.Operation, o.State].filter(v => v != null).map(v => dbValue(v)).join(" · ")}
      actions={o => <>
        {orderLink(o) && <Link className="btn" to={orderLink(o)!}>Карточка заявки</Link>}
        {o.ClassCode != null && o.SecCode != null && <Link className="btn-muted" to={instrumentPath(o.ClassCode, o.SecCode)}>Инструмент</Link>}
      </>} />
  </div>;
}
