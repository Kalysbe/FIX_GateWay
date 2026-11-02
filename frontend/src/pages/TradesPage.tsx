import { useMemo, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, nextOrderOffset, ORDER_PAGE_SIZE, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { rawColumns, rawSearchText } from "../components/rawColumns";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable } from "../components/DataTable";
import { FirmSelect } from "../components/FirmSelect";

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
  const columns = useMemo(() => rawColumns(trades), [trades]);
  const changeFirm = (value: string) => { setFirm(value); setOffsets([0]); };
  const first = offset + 1, last = offset + trades.length;
  return <div className="raw-orders space-y-4">
    <PageHeader title="Сделки" subtitle="Строки таблицы QUIK Trades в том виде, в каком они хранятся в базе" />
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Сделки" rows={q.error ? [] : trades} columns={columns} rowKey={t => [dbValue(t.TradeDate), dbValue(t.ClassCode), dbValue(t.TradeNum), dbValue(t.Operation), dbValue(t.FirmId), dbValue(t.Account)].join("\u0000")} getSearchText={rawSearchText(trades)} loading={q.isLoading} resetKey={`${firm}:${offset}`} exportName="trades" showTotals={false} onRefresh={() => void q.refetch()} refreshing={q.isFetching} searchPlaceholder="Поиск по загруженной странице…" emptyTitle={q.error ? "Сделки временно недоступны" : "Сделок нет"} filters={<FirmSelect label="Фирма" emptyLabel="Все фирмы" value={firm} onChange={changeFirm} />} />
    <div className="flex items-center justify-end gap-2 text-xs text-muted">
      <span aria-live="polite">Строки БД {trades.length ? `${first}–${last}` : "—"}</span>
      <button type="button" className="btn-muted" disabled={offsets.length < 2 || q.isFetching} onClick={() => setOffsets(o => o.slice(0, -1))}>← Предыдущие {ORDER_PAGE_SIZE}</button>
      <button type="button" className="btn-muted" disabled={q.data?.next == null || q.isFetching} onClick={() => { const next = q.data?.next; if (next != null) setOffsets(o => [...o, next]); }}>Следующие {ORDER_PAGE_SIZE} →</button>
    </div>
  </div>;
}
