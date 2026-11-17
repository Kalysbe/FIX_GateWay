import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, nextOrderOffset, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable } from "../components/DataTable";
import { rawSearchText } from "../components/rawColumns";
import { formatQuik, quikColumns, type QuikColumn } from "../components/quikColumns";
import { instrumentPath } from "../lib/quotes";
import { useUrlState } from "../lib/urlState";
import { RowDrawer } from "../components/RowDrawer";

// Today's rows of QUIK quik_export2.public."Params" of the chosen classes with
// instrument and class names; the server selects the columns. Numbers keep
// their source text in sorting and CSV.
type ParamRow = DBOrder;

// A bond market at a glance; depth, lot, step and the rest are in the column
// menu. Each instrument links to its own page with all of its market data.
const PARAM_COLUMNS: QuikColumn[] = [
  { id: "instrument", header: "Инструмент", field: "SecCode", sub: "SecShortName", link: r => instrumentPath(r.ClassCode, r.SecCode) },
  { id: "class", header: "Класс / валюта", field: "ClassCode", sub: "FaceUnit" },
  { id: "status", header: "Торги", field: "tradingstatus", kind: "state", sub: "status" },
  { id: "bid", header: "Спрос", field: "bid", kind: "number", frac: 2, trend: true },
  { id: "offer", header: "Предложение", field: "offer", kind: "number", frac: 2, trend: true },
  { id: "last", header: "Последняя", field: "last", kind: "number", frac: 2, sub: "time", subKind: "time", trend: true },
  { id: "waprice", header: "Средневзв.", field: "waprice", kind: "number", frac: 2, trend: true },
  { id: "yield", header: "Дох-ть, %", field: "yield", kind: "number", frac: 2, trend: true },
  { id: "accrued", header: "НКД", field: "accruedint", kind: "number", frac: 2 },
  { id: "coupon", header: "Купон / дата", field: "CouponValue", kind: "number", frac: 2, sub: "nextcoupon", subKind: "date" },
  { id: "couponperiod", header: "Период купона, дн.", field: "couponperiod", kind: "number", hidden: true },
  { id: "limits", header: "Цена мин – макс", field: "pricemin", kind: "number", frac: 2, render: (r, min) => <span className="whitespace-nowrap">{min} – {formatQuik(r.pricemax, "number", 2)}</span> },
  { id: "numtrades", header: "Сделок", field: "numtrades", kind: "number" },
  { id: "voltoday", header: "Кол-во", field: "voltoday", kind: "number" },
  { id: "valtoday", header: "Оборот", field: "valtoday", kind: "number", frac: 2 },
  { id: "open", header: "Открытие", field: "open", kind: "number", frac: 2, hidden: true },
  { id: "high", header: "Макс.", field: "high", kind: "number", frac: 2, hidden: true },
  { id: "low", header: "Мин.", field: "low", kind: "number", frac: 2, hidden: true },
  { id: "prevprice", header: "Пред. цена", field: "prevprice", kind: "number", frac: 2, hidden: true },
];

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
  const [board, setBoard] = useUrlState("board");
  const [open, setOpen] = useState<ParamRow | null>(null);
  const [visibleOrder, setVisibleOrder] = useState<ParamRow[]>([]);
  // marketdata.tick invalidates this query (throttled in AdminStreamProvider).
  const q = useQuery({
    queryKey: ["admin-market-data"],
    queryFn: ({ signal }) => loadParams(signal),
    refetchInterval: liveRefetchInterval(connected, 2000, 10000),
  });
  const rows = q.data ?? [];
  const boards = useMemo(() => Array.from(new Set(rows.map(r => dbValue(r.ClassCode)).filter(v => v !== "null"))).sort(), [rows]);
  const columns = useMemo(() => quikColumns(rows, PARAM_COLUMNS), [rows]);
  const filtered = board ? rows.filter(r => dbValue(r.ClassCode) === board) : rows;
  const latest = new Map(rows.map(r => [paramKey(r), r]));
  const drawerRows = visibleOrder.map(r => latest.get(paramKey(r))).filter((r): r is ParamRow => !!r);
  return <div className="raw-orders space-y-4">
    <PageHeader title="Текущие торги" subtitle="Инструменты выбранных классов QUIK за сегодняшний торговый день: Params, Securities и Classes" />
    <p className="text-xs text-muted">Какие классы показывать, задаётся на странице <Link className="text-accent" to="/classes">«Классы QUIK»</Link>.</p>
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    <DataTable title="Текущие торги" rows={q.error ? [] : filtered} columns={columns} rowKey={paramKey} loading={q.isLoading} resetKey={board} exportName="market-data" showTotals={false}
      getSearchText={rawSearchText(rows)} onRefresh={() => void q.refetch()} refreshing={q.isFetching}
      live urlState selection={{}} activeRowKey={open ? paramKey(open) : null} onRowClick={(row, sorted) => { setVisibleOrder(sorted); setOpen(row); }}
      searchPlaceholder="Поиск по инструментам…" emptyTitle={q.error ? "Таблица Params недоступна" : "Нет строк выбранных классов за сегодня"}
      filters={<>
        <select className="input w-auto" aria-label="ClassCode" value={board} onChange={e => setBoard(e.target.value)}><option value="">ClassCode: все</option>{boards.map(b => <option key={b} value={b}>{b}</option>)}</select>
        {board && <button className="btn-muted" onClick={() => setBoard("")}>Сбросить</button>}
      </>} />
    <RowDrawer row={open} rows={drawerRows} rowKey={paramKey} onChange={setOpen}
      title={r => `${dbValue(r.SecCode)} · ${dbValue(r.ClassCode)}`}
      subtitle={r => [r.SecShortName, r.ClassName].filter(v => v != null).map(v => dbValue(v)).join(" · ")}
      actions={r => r.ClassCode != null && r.SecCode != null ? <Link className="btn" to={instrumentPath(r.ClassCode, r.SecCode)}>Страница инструмента</Link> : null} />
    <div className="text-xs text-muted" aria-live="polite">Строк: {rows.length}{rows.length >= MAX_ROWS ? ` (показаны первые ${MAX_ROWS})` : ""}</div>
  </div>;
}
