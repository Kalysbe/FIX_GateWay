import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbSortValue, dbValue, nextOrderOffset, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable, type TableColumn } from "../components/DataTable";

// Rows of QUIK quik_export2.public."Params" exactly as stored: column names and
// values come from the database, numbers keep their source text.
type ParamRow = DBOrder;

const PAGE = 1000;
// The page polls a whole table; this bounds a runaway source, not a real market.
const MAX_ROWS = 20000;

export function paramKey(row: ParamRow): string {
  return row.ClassCode == null || row.SecCode == null ? JSON.stringify(row) : `${row.ClassCode}\u0000${row.SecCode}`;
}

async function loadParams(signal: AbortSignal): Promise<ParamRow[]> {
  const rows: ParamRow[] = [];
  for (let offset: number | null = 0; offset != null && rows.length < MAX_ROWS;) {
    const { data, headers } = await apiWithHeaders<ParamRow[]>(`/api/v1/admin/market-data?limit=${PAGE}&offset=${offset}`, { signal }, parseDBJSON);
    rows.push(...(data ?? []));
    offset = nextOrderOffset(headers);
  }
  return rows;
}

export default function MarketDataPage() {
  const { connected } = useAdminStream();
  const [board, setBoard] = useState("");
  // marketdata.tick invalidates this query (throttled in AdminStreamProvider).
  const q = useQuery({
    queryKey: ["admin-market-data"],
    queryFn: ({ signal }) => loadParams(signal),
    refetchInterval: liveRefetchInterval(connected, 2000, 10000),
  });
  const rows = q.data ?? [];
  const boards = useMemo(() => Array.from(new Set(rows.map(r => dbValue(r.ClassCode)).filter(v => v !== "null"))).sort(), [rows]);
  const fields = useMemo(() => Array.from(new Set(rows.flatMap(r => Object.keys(r)))), [rows]);
  const filtered = board ? rows.filter(r => dbValue(r.ClassCode) === board) : rows;
  const columns: TableColumn<ParamRow>[] = fields.map(field => ({
    id: field, header: field, value: r => dbSortValue(r[field]), exportValue: r => dbValue(r[field]),
    cell: r => <span className="whitespace-pre-wrap">{dbValue(r[field])}</span>,
  }));
  return <div className="raw-orders space-y-4">
    <PageHeader title="Текущие торги" subtitle="Строки таблицы QUIK Params в том виде, в каком они хранятся в базе" />
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Текущие торги" rows={q.error ? [] : filtered} columns={columns} rowKey={paramKey} loading={q.isLoading} resetKey={board} exportName="market-data" showTotals={false}
      getSearchText={r => fields.map(f => dbValue(r[f])).join(" ")} onRefresh={() => void q.refetch()} refreshing={q.isFetching}
      searchPlaceholder="Поиск по строкам Params…" emptyTitle={q.error ? "Таблица Params недоступна" : "В Params нет строк"}
      filters={<>
        <select className="input w-auto" aria-label="ClassCode" value={board} onChange={e => setBoard(e.target.value)}><option value="">ClassCode: все</option>{boards.map(b => <option key={b} value={b}>{b}</option>)}</select>
        {board && <button className="btn-muted" onClick={() => setBoard("")}>Сбросить</button>}
      </>} />
    <div className="text-xs text-muted" aria-live="polite">Строк Params: {rows.length}{rows.length >= MAX_ROWS ? ` (показаны первые ${MAX_ROWS})` : ""}</div>
  </div>;
}
