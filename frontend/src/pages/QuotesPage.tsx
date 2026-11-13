import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, nextOrderOffset, orderRowKey, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { instrumentPath, isBuy, quoteColumns } from "../lib/quotes";
import { orderKey } from "../lib/rawOrders";
import { useUrlState } from "../lib/urlState";
import { RowDrawer } from "../components/RowDrawer";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable } from "../components/DataTable";

const PAGE = 1000;
// Bounds a runaway source; a real order book is far smaller.
const MAX_ROWS = 20000;

// Quotes are today's QUIK Orders rows still in the book (State = "Активна"),
// read by the gateway from the QUIK export database.
async function loadQuotes(signal: AbortSignal): Promise<DBOrder[]> {
  const rows: DBOrder[] = [];
  for (let offset: number | null = 0; offset != null && rows.length < MAX_ROWS;) {
    const { data, headers } = await apiWithHeaders<DBOrder[]>(`/api/v1/admin/quotes?limit=${PAGE}&offset=${offset}`, { signal }, parseDBJSON);
    rows.push(...(data ?? []));
    offset = nextOrderOffset(headers);
  }
  return rows;
}

export default function QuotesPage() {
  const { connected } = useAdminStream();
  const [opFilter, setOpFilter] = useUrlState("op");
  const [classFilter, setClassFilter] = useUrlState("class");
  const [open, setOpen] = useState<DBOrder | null>(null);
  const [visibleOrder, setVisibleOrder] = useState<DBOrder[]>([]);

  const quotesQ = useQuery({
    queryKey: ["admin-quotes"],
    queryFn: ({ signal }) => loadQuotes(signal),
    // order.updated refreshes the quotes while the event stream is up; polling is the fallback.
    refetchInterval: liveRefetchInterval(connected, 5000),
  });

  const quotes = quotesQ.data ?? [];
  const classOptions = useMemo(() => Array.from(new Set(quotes.map(r => dbValue(r.ClassCode)).filter(v => v !== "null"))).sort(), [quotes]);
  const operations = useMemo(() => Array.from(new Set(quotes.map(r => dbValue(r.Operation)).filter(v => v !== "null"))), [quotes]);
  const filtered = useMemo(() => quotes.filter(r =>
    (!opFilter || dbValue(r.Operation) === opFilter) && (!classFilter || dbValue(r.ClassCode) === classFilter)), [quotes, opFilter, classFilter]);
  const columns = quoteColumns();
  const latest = new Map(quotes.map(r => [orderRowKey(r), r]));
  const drawerRows = visibleOrder.map(r => latest.get(orderRowKey(r))).filter((r): r is DBOrder => !!r);

  return (
    <div className="space-y-4">
      <PageHeader
        title="Котировки"
        subtitle="Сегодняшние активные заявки QUIK · таблица Orders, State = «Активна»"
        actions={
          <div className="rounded-lg border border-line bg-panel px-3 py-1.5 text-xs text-ink">
            <span className="mr-2 text-muted">Строк</span>
            <span className="font-mono font-semibold">{filtered.length}</span>
          </div>
        }
      />

      {quotesQ.isError && <ErrorBanner error={quotesQ.error} onRetry={() => void quotesQ.refetch()} />}
      <DataTable title="Котировки QUIK" rows={quotesQ.isError ? [] : filtered} columns={columns} rowKey={orderRowKey} loading={quotesQ.isLoading} resetKey={opFilter + ":" + classFilter} exportName="quotes" onRefresh={() => void quotesQ.refetch()} refreshing={quotesQ.isFetching} searchPlaceholder="Номер заявки, фирма, класс, инструмент…" emptyTitle={quotesQ.isError ? "Таблица Orders недоступна" : "Нет активных заявок за сегодня"} getSearchText={r => [r.OrderNum, r.FirmId, r.ClassCode, r.SecCode, r.Operation].map(dbValue).join(" ")}
        live urlState selection={{}} activeRowKey={open ? orderRowKey(open) : null} onRowClick={(row, sorted) => { setVisibleOrder(sorted); setOpen(row); }} filters={<>
        <select className="input w-auto" aria-label="Операция" value={opFilter} onChange={e => setOpFilter(e.target.value)}><option value="">Все операции</option>{operations.map(op => <option key={op} value={op}>{op}</option>)}</select>
        <select className="input w-auto" aria-label="Класс котировки" value={classFilter} onChange={e => setClassFilter(e.target.value)}><option value="">Все классы</option>{classOptions.map(c => <option key={c} value={c}>{c}</option>)}</select>
        {(opFilter || classFilter) && <button className="btn-muted" onClick={() => { setOpFilter(""); setClassFilter(""); }}>Сбросить</button>}
      </>} summary={rows => [{ label: "Заявок", value: rows.length }, { label: "Покупка", value: rows.filter(r => isBuy(r.Operation)).length, tone: "success" }, { label: "Продажа", value: rows.filter(r => !isBuy(r.Operation)).length }, { label: "Инструментов", value: new Set(rows.map(r => dbValue(r.ClassCode) + ":" + dbValue(r.SecCode))).size }]} />
      <RowDrawer row={open} rows={drawerRows} rowKey={orderRowKey} onChange={setOpen}
        title={r => `Котировка · заявка ${dbValue(r.OrderNum)}`}
        subtitle={r => [r.SecCode, r.Operation, r.FirmId].filter(v => v != null).map(v => dbValue(v)).join(" · ")}
        actions={r => <>
          {orderKey(r) && <Link className="btn" to={`/orders/${encodeURIComponent(orderKey(r))}`}>Карточка заявки</Link>}
          {r.ClassCode != null && r.SecCode != null && <Link className="btn-muted" to={instrumentPath(r.ClassCode, r.SecCode)}>Инструмент</Link>}
        </>} />
    </div>
  );
}
