import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, nextOrderOffset, orderRowKey, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { isBuy, quoteColumns } from "../lib/quotes";
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
  const [opFilter, setOpFilter] = useState("");
  const [classFilter, setClassFilter] = useState("");

  const quotesQ = useQuery({
    queryKey: ["admin-quotes"],
    queryFn: ({ signal }) => loadQuotes(signal),
    refetchInterval: liveRefetchInterval(false, 5000, 8000),
  });

  const quotes = quotesQ.data ?? [];
  const classOptions = useMemo(() => Array.from(new Set(quotes.map(r => dbValue(r.ClassCode)).filter(v => v !== "null"))).sort(), [quotes]);
  const operations = useMemo(() => Array.from(new Set(quotes.map(r => dbValue(r.Operation)).filter(v => v !== "null"))), [quotes]);
  const filtered = useMemo(() => quotes.filter(r =>
    (!opFilter || dbValue(r.Operation) === opFilter) && (!classFilter || dbValue(r.ClassCode) === classFilter)), [quotes, opFilter, classFilter]);
  const columns = quoteColumns();

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
      <DataTable title="Котировки QUIK" rows={quotesQ.isError ? [] : filtered} columns={columns} rowKey={orderRowKey} loading={quotesQ.isLoading} resetKey={opFilter + ":" + classFilter} exportName="quotes" onRefresh={() => void quotesQ.refetch()} refreshing={quotesQ.isFetching} searchPlaceholder="Номер заявки, фирма, класс, инструмент…" emptyTitle={quotesQ.isError ? "Таблица Orders недоступна" : "Нет активных заявок за сегодня"} getSearchText={r => [r.OrderNum, r.FirmId, r.ClassCode, r.SecCode, r.Operation].map(dbValue).join(" ")} filters={<>
        <select className="input w-auto" aria-label="Операция" value={opFilter} onChange={e => setOpFilter(e.target.value)}><option value="">Все операции</option>{operations.map(op => <option key={op} value={op}>{op}</option>)}</select>
        <select className="input w-auto" aria-label="Класс котировки" value={classFilter} onChange={e => setClassFilter(e.target.value)}><option value="">Все классы</option>{classOptions.map(c => <option key={c} value={c}>{c}</option>)}</select>
        {(opFilter || classFilter) && <button className="btn-muted" onClick={() => { setOpFilter(""); setClassFilter(""); }}>Сбросить</button>}
      </>} summary={rows => [{ label: "Заявок", value: rows.length }, { label: "Покупка", value: rows.filter(r => isBuy(r.Operation)).length, tone: "success" }, { label: "Продажа", value: rows.filter(r => !isBuy(r.Operation)).length }, { label: "Инструментов", value: new Set(rows.map(r => dbValue(r.ClassCode) + ":" + dbValue(r.SecCode))).size }]} />
    </div>
  );
}
