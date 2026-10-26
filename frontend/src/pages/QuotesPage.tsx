import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, apiWithHeaders, getSessionUser } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, nextOrderOffset, orderRowKey, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { isBuy, quoteColumns } from "../lib/quotes";
import { useToast } from "../components/Toast";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable } from "../components/DataTable";

type SettingsResp = {
  base_url: string;
  default_base_url: string;
};

const WRITE_ROLES = new Set(["SUPER_ADMIN", "ADMIN", "OPERATOR"]);
const PAGE = 1000;
// Bounds a runaway source; a real order book is far smaller.
const MAX_ROWS = 20000;

function canEditURL(role?: string | null): boolean {
  return WRITE_ROLES.has((role || "").toUpperCase());
}

// Quotes are QUIK Orders rows still in the book (State = "Активна"), read by
// the gateway from the QUIK export database.
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
  const qc = useQueryClient();
  const toast = useToast();
  const canWrite = canEditURL(getSessionUser()?.role);
  const [opFilter, setOpFilter] = useState("");
  const [classFilter, setClassFilter] = useState("");
  const [draft, setDraft] = useState("");
  const [touched, setTouched] = useState(false);

  const settingsQ = useQuery({
    queryKey: ["quik-api-settings"],
    queryFn: ({ signal }) => api<SettingsResp>("/api/v1/admin/quik-api/settings", { signal }),
  });

  const quotesQ = useQuery({
    queryKey: ["admin-quotes"],
    queryFn: ({ signal }) => loadQuotes(signal),
    refetchInterval: liveRefetchInterval(false, 5000, 8000),
  });

  useEffect(() => {
    if (!touched && settingsQ.data?.base_url) {
      setDraft(settingsQ.data.base_url);
    }
  }, [settingsQ.data, touched]);

  const save = useMutation({
    mutationFn: () =>
      api<SettingsResp>("/api/v1/admin/quik-api/settings", {
        method: "PATCH",
        body: JSON.stringify({ base_url: draft }),
      }),
    onSuccess: (data) => {
      setDraft(data.base_url);
      setTouched(false);
      toast.push("Адрес QUIK API сохранён", "ok");
      qc.invalidateQueries({ queryKey: ["quik-api-settings"] });
    },
    onError: (err) => toast.push(err instanceof Error ? err.message : "Не удалось сохранить адрес"),
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
        subtitle="Активные заявки QUIK · таблица Orders, State = «Активна»"
        actions={
          <div className="rounded-lg border border-line bg-panel px-3 py-1.5 text-xs text-ink">
            <span className="mr-2 text-slate-500">Строк</span>
            <span className="font-mono font-semibold">{filtered.length}</span>
          </div>
        }
      />

      {quotesQ.isError && <ErrorBanner error={quotesQ.error} onRetry={() => void quotesQ.refetch()} />}
      <DataTable title="Котировки QUIK" rows={quotesQ.isError ? [] : filtered} columns={columns} rowKey={orderRowKey} loading={quotesQ.isLoading} resetKey={opFilter + ":" + classFilter} exportName="quotes" onRefresh={() => void quotesQ.refetch()} refreshing={quotesQ.isFetching} searchPlaceholder="Номер заявки, фирма, класс, инструмент…" emptyTitle={quotesQ.isError ? "Таблица Orders недоступна" : "Нет активных заявок"} getSearchText={r => [r.OrderNum, r.FirmId, r.ClassCode, r.SecCode, r.Operation].map(dbValue).join(" ")} filters={<>
        <select className="input w-auto" aria-label="Операция" value={opFilter} onChange={e => setOpFilter(e.target.value)}><option value="">Все операции</option>{operations.map(op => <option key={op} value={op}>{op}</option>)}</select>
        <select className="input w-auto" aria-label="Класс котировки" value={classFilter} onChange={e => setClassFilter(e.target.value)}><option value="">Все классы</option>{classOptions.map(c => <option key={c} value={c}>{c}</option>)}</select>
        {(opFilter || classFilter) && <button className="btn-muted" onClick={() => { setOpFilter(""); setClassFilter(""); }}>Сбросить</button>}
      </>} summary={rows => [{ label: "Заявок", value: rows.length }, { label: "Покупка", value: rows.filter(r => isBuy(r.Operation)).length, tone: "success" }, { label: "Продажа", value: rows.filter(r => !isBuy(r.Operation)).length }, { label: "Инструментов", value: new Set(rows.map(r => dbValue(r.ClassCode) + ":" + dbValue(r.SecCode))).size }]} />

      <div className="card space-y-3">
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (canWrite) save.mutate();
          }}
        >
          <label className="min-w-[280px] flex-1">
            <div className="mb-1 text-[12px] font-medium text-muted">Адрес QUIK API (история сделок)</div>
            <input
              className="input font-mono"
              placeholder="http://192.168.0.89:5000"
              value={draft}
              disabled={!canWrite || save.isPending}
              onChange={(e) => {
                setTouched(true);
                setDraft(e.target.value);
              }}
            />
          </label>
          {canWrite ? (
            <button className="btn" type="submit" disabled={save.isPending || !draft.trim()}>
              {save.isPending ? "Сохранение…" : "Сохранить"}
            </button>
          ) : null}
        </form>
        <p className="text-[12px] text-muted">
          Котировки читаются из БД QUIK. Этот адрес используется только для загрузки истории сделок; достаточно{" "}
          <span className="font-mono">http://192.168.0.89</span> или с портом <span className="font-mono">http://192.168.0.89:5000</span>.
        </p>
      </div>
    </div>
  );
}
