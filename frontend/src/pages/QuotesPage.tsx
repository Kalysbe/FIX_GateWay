import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, getSessionUser } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { useToast } from "../components/Toast";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable, type TableColumn } from "../components/DataTable";
import { numberValue } from "../lib/table";

type QuikQuote = {
  operation: string;
  order_num: string;
  class_code: string;
  sec_code: string;
  price: string;
  value: string;
  qty: string;
};

type QuotesResp = {
  source?: string;
  base_url?: string;
  count: number;
  quotes: QuikQuote[];
};

type SettingsResp = {
  base_url: string;
  default_base_url: string;
};

const WRITE_ROLES = new Set(["SUPER_ADMIN", "ADMIN", "OPERATOR"]);

function canEditURL(role?: string | null): boolean {
  return WRITE_ROLES.has((role || "").toUpperCase());
}

function fmt(v: string | number | null | undefined, digits = 2): string {
  if (v == null || v === "") return "—";
  const n = Number(v);
  if (!Number.isFinite(n)) return String(v);
  return n.toLocaleString("ru-RU", {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits,
  });
}

function isBuy(op: string): boolean {
  const s = (op || "").toLowerCase();
  return s === "buy" || s.includes("купл");
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
    queryKey: ["quik-api-quotes"],
    queryFn: ({ signal }) => api<QuotesResp>("/api/v1/admin/quik-api/quotes", { signal }),
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
      qc.invalidateQueries({ queryKey: ["quik-api-quotes"] });
    },
    onError: (err) => toast.push(err instanceof Error ? err.message : "Не удалось сохранить адрес"),
  });

  const quotes = quotesQ.data?.quotes || [];
  const classOptions = useMemo(() => {
    const set = new Set<string>();
    quotes.forEach((r) => {
      if (r.class_code) set.add(r.class_code);
    });
    return Array.from(set).sort();
  }, [quotes]);

  const filtered = useMemo(() => {
    return quotes.filter((r) => {
      if (opFilter && r.operation !== opFilter) return false;
      if (classFilter && r.class_code !== classFilter) return false;
      return true;
    });
  }, [quotes, opFilter, classFilter]);

  const operations = useMemo(() => {
    const set = new Set<string>();
    quotes.forEach((r) => {
      if (r.operation) set.add(r.operation);
    });
    return Array.from(set);
  }, [quotes]);

  const columns: TableColumn<QuikQuote>[] = [
    { id: "operation", header: "Операция", value: r => r.operation, cell: r => <span className={isBuy(r.operation) ? "badge-success" : "badge-danger"}>{r.operation || "—"}</span> },
    { id: "number", header: "№ заявки", value: r => r.order_num, className: "font-mono" },
    { id: "class", header: "Класс", value: r => r.class_code, className: "font-mono" },
    { id: "symbol", header: "Инструмент", value: r => r.sec_code, className: "font-mono font-medium" },
    { id: "price", header: "Цена", value: r => numberValue(r.price), cell: r => fmt(r.price), numeric: true },
    { id: "value", header: "Сумма", value: r => numberValue(r.value), cell: r => fmt(r.value), numeric: true },
    { id: "quantity", header: "Количество", value: r => numberValue(r.qty), cell: r => fmt(r.qty, 0), numeric: true },
  ];

  return (
    <div className="space-y-4">
      <PageHeader
        title="Котировки"
        subtitle="Активные заявки QUIK Module · GET /api/quotes"
        actions={
          <div className="flex flex-wrap gap-2 text-xs">
            <div className="rounded-lg border border-line bg-panel px-3 py-1.5 text-ink">
              <span className="mr-2 text-slate-500">Источник</span>
              <span className="font-mono">{quotesQ.data?.base_url || settingsQ.data?.base_url || "—"}</span>
            </div>
            <div className="rounded-lg border border-line bg-panel px-3 py-1.5 text-ink">
              <span className="mr-2 text-slate-500">Строк</span>
              <span className="font-mono font-semibold">{filtered.length}</span>
            </div>
          </div>
        }
      />

      <div className="card space-y-3">
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (canWrite) save.mutate();
          }}
        >
          <label className="min-w-[280px] flex-1">
            <div className="mb-1 text-[12px] font-medium text-muted">Адрес внутреннего API</div>
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
          <button
            type="button"
            className="btn-muted"
            onClick={() => void quotesQ.refetch()}
          >
            Обновить
          </button>
        </form>
        <p className="text-[12px] text-muted">
          Хост меняется — достаточно <span className="font-mono">http://192.168.0.89</span> или с портом{" "}
          <span className="font-mono">http://192.168.0.89:5000</span>. Шлюз сам запросит{" "}
          <span className="font-mono">/api/quotes</span>.
        </p>
      </div>

      {quotesQ.isError && <ErrorBanner error={quotesQ.error} onRetry={() => void quotesQ.refetch()} />}
      <DataTable title="Котировки QUIK" rows={filtered} columns={columns} rowKey={r => [r.order_num, r.sec_code, r.class_code].join("|")} loading={quotesQ.isLoading} resetKey={opFilter + ":" + classFilter} exportName="quotes" onRefresh={() => void quotesQ.refetch()} refreshing={quotesQ.isFetching} searchPlaceholder="Номер заявки, класс, инструмент…" emptyTitle="Нет активных котировок" emptyHint="Проверьте подключение к внутреннему API" filters={<>
        <select className="input w-auto" aria-label="Операция" value={opFilter} onChange={e => setOpFilter(e.target.value)}><option value="">Все операции</option>{operations.map(op => <option key={op} value={op}>{op}</option>)}</select>
        <select className="input w-auto" aria-label="Класс котировки" value={classFilter} onChange={e => setClassFilter(e.target.value)}><option value="">Все классы</option>{classOptions.map(c => <option key={c} value={c}>{c}</option>)}</select>
        {(opFilter || classFilter) && <button className="btn-muted" onClick={() => { setOpFilter(""); setClassFilter(""); }}>Сбросить</button>}
      </>} summary={rows => [{ label: "Заявок", value: rows.length }, { label: "Покупка", value: rows.filter(r => isBuy(r.operation)).length, tone: "success" }, { label: "Продажа", value: rows.filter(r => !isBuy(r.operation)).length }, { label: "Инструментов", value: new Set(rows.map(r => r.class_code + ":" + r.sec_code)).size }]} />
    </div>
  );
}
