import { useMemo, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, nextOrderOffset, ORDER_PAGE_SIZE, orderKey, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { instrumentPath } from "../lib/quotes";
import { rawSearchText } from "../components/rawColumns";
import { quikColumns, type QuikColumn } from "../components/quikColumns";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable } from "../components/DataTable";
import { FirmSelect } from "../components/FirmSelect";

// The order of a trade shares its TradeDate and ClassCode.
const orderLink = (t: DBOrder) => orderKey(t) ? `/orders/${encodeURIComponent(orderKey(t))}` : undefined;

// What a bond trade needs at a glance; every other Trades column is in the
// column menu.
const TRADE_COLUMNS: QuikColumn[] = [
  { id: "num", header: "Сделка", field: "TradeNum" },
  { id: "time", header: "Время", field: ["TradeDateTime", "DateTime", "TradeTime", "TradeDate"], kind: "datetime" },
  { id: "instrument", header: "Инструмент", field: "SecCode", sub: "ClassCode", link: t => instrumentPath(t.ClassCode, t.SecCode) },
  { id: "side", header: "Операция", field: "Operation", kind: "side" },
  { id: "price", header: "Цена", field: "Price", kind: "number", frac: 2 },
  { id: "yield", header: "Дох-ть, %", field: "Yield", kind: "number", frac: 2 },
  { id: "qty", header: "Кол-во", field: "Qty", kind: "number" },
  { id: "value", header: "Объём", field: "Value", kind: "number", frac: 2 },
  { id: "accrued", header: "НКД", field: "Accruedint", kind: "number", frac: 2 },
  { id: "currency", header: "Валюта", field: ["PriceCurrency", "SettleCurrency"] },
  { id: "order", header: "Заявка", field: "OrderNum", link: orderLink },
  { id: "account", header: "Счёт / клиент", field: "Account", sub: "ClientCode" },
  { id: "firm", header: "Фирма", field: "FirmId", sub: "FirmName" },
  { id: "settle", header: "Расчёты", field: "SettleDate", kind: "date" },
];

// Rows of QUIK "Trades" as stored, one server page at a time; each row is one
// side of a trade. The Gateway keeps no copy of them.
export default function TradesPage() {
  const { connected } = useAdminStream();
  const [firm, setFirm] = useState("");
  const [offsets, setOffsets] = useState<number[]>([0]);
  const offset = offsets[offsets.length - 1];
  const q = useQuery({
    queryKey: ["admin-trades", firm, offset],
    queryFn: async ({ signal }) => {
      const { data, headers } = await apiWithHeaders<DBOrder[]>(`/api/v1/admin/trades?limit=${ORDER_PAGE_SIZE}&offset=${offset}${firm ? `&firm_id=${encodeURIComponent(firm)}` : ""}`, { signal }, parseDBJSON);
      return { rows: data ?? [], next: nextOrderOffset(headers) };
    },
    placeholderData: keepPreviousData,
    refetchInterval: liveRefetchInterval(connected, 5000),
  });
  const trades = q.data?.rows ?? [];
  const columns = useMemo(() => quikColumns(trades, TRADE_COLUMNS), [trades]);
  const changeFirm = (value: string) => { setFirm(value); setOffsets([0]); };
  const serverPage = { start: offset + 1, end: offset + trades.length, size: ORDER_PAGE_SIZE, hasPrevious: offsets.length > 1, hasNext: q.data?.next != null, busy: q.isFetching,
    onPrevious: () => setOffsets(o => o.slice(0, -1)), onNext: () => { const next = q.data?.next; if (next != null) setOffsets(o => [...o, next]); } };
  return <div className="raw-orders space-y-4">
    <PageHeader title="Сделки" subtitle="Таблица QUIK Trades: каждая строка — сторона сделки. Остальные поля — в меню столбцов" />
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Сделки" rows={q.error ? [] : trades} columns={columns} rowKey={t => [dbValue(t.TradeDate), dbValue(t.ClassCode), dbValue(t.TradeNum), dbValue(t.Operation), dbValue(t.FirmId), dbValue(t.Account)].join("\u0000")} getSearchText={rawSearchText(trades)} loading={q.isLoading} resetKey={`${firm}:${offset}`} exportName="trades" showTotals={false} onRefresh={() => void q.refetch()} refreshing={q.isFetching} searchPlaceholder="Поиск по загруженной странице…" emptyTitle={q.error ? "Сделки временно недоступны" : "Сделок нет"} filters={<FirmSelect label="Фирма" emptyLabel="Все фирмы" value={firm} onChange={changeFirm} />} serverPage={serverPage} />
  </div>;
}
