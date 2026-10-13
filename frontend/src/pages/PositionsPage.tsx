import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { DataTable, type TableColumn } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";
import { numberValue } from "../lib/table";

type FIXPos = {
  adapter_id?: string;
  pos_maint_rpt_id?: string;
  pos_req_id?: string;
  clearing_date?: string;
  kind: string;
  firm_id: string;
  client_code: string;
  account_code: string;
  symbol: string;
  security_id?: string;
  board: string;
  currency: string;
  pos_type?: string;
  update_action?: string;
  source?: string;
  keys?: Record<string, string>;
  items?: Record<string, string>;
  long_qty: string | number;
  short_qty: string | number;
  net_qty: string | number;
  pos_amt_type?: string;
  pos_amt?: string | number;
  settl_price?: string | number;
};

type Snapshot = {
  id: number;
  adapter_id: string;
  pos_req_id: string;
  clearing_date: string;
  requested_at: string;
  completed_at?: string;
  status: string;
  report_count: number;
};

type PositionsResp = {
  source?: string;
  note?: string;
  snapshot?: Snapshot;
  instruments: FIXPos[];
  cash: FIXPos[];
  live?: boolean;
  subscription?: PositionSubscription;
};

type PositionSubscription = { adapter_id?: string; pos_req_id?: string; status: string; mode?: string; positions: number; error?: string; next_retry_at?: string; last_received_at?: string };
const subscriptionLabels: Record<string, string> = { ACTIVE: "Подписка активна", STARTING: "Получение позиций", WAITING: "Ожидание FIX-связи", RETRYING: "Восстановление позиций", OFFLINE: "Ожидание FIX-связи" };

function fmt(v: string | number | null | undefined, digits = 2): string {
  if (v == null || v === "") return "—";
  const n = Number(v);
  if (!Number.isFinite(n)) return String(v);
  return n.toLocaleString("ru-RU", {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits,
  });
}

function item(r: FIXPos, key: string): string {
  return r.items?.[key] ?? "";
}

function netTone(v: string | number | null | undefined): string {
  const n = Number(v);
  if (!Number.isFinite(n) || n === 0) return "text-slate-600";
  return n > 0 ? "text-emerald-700" : "text-rose-700";
}

function posTypeLabel(t?: string): string {
  switch ((t || "").toUpperCase()) {
    case "ML":
      return "ML · деньги";
    case "DL":
      return "DL · инструмент";
    case "FL":
      return "FL · фьюч.лимит";
    case "FP":
      return "FP · фьюч.поз.";
    default:
      return t || "—";
  }
}

export default function PositionsPage() {
  const { connected } = useAdminStream();
  const [brokerFilter, setBrokerFilter] = useState("");
  const [firmFilter, setFirmFilter] = useState("");
  const [clientFilter, setClientFilter] = useState("");
  const [tab, setTab] = useState<"instruments" | "cash">("instruments");

  const brokersQ = useQuery({
    queryKey: ["brokers"],
    queryFn: () => api<{ brokers: Array<{ id: number; name: string; code: string }> }>("/api/v1/admin/brokers"),
  });

  const positions = useQuery({
    queryKey: ["admin-positions", brokerFilter],
    queryFn: ({ signal }) => {
      const qs = brokerFilter ? `?broker_id=${encodeURIComponent(brokerFilter)}` : "";
      return api<PositionsResp>(`/api/v1/admin/positions${qs}`, { signal });
    },
    refetchInterval: liveRefetchInterval(connected, 2000, 5000),
  });

  const instruments = positions.data?.instruments || [];
  const cash = positions.data?.cash || [];
  const snapshot = positions.data?.snapshot;
  const subscription = positions.data?.subscription;

  const brokers = useMemo(() => {
    return (brokersQ.data?.brokers || [])
      .map((b) => [String(b.id), `${b.name} (${b.code})`] as const)
      .sort((a, b) => a[1].localeCompare(b[1], "ru"));
  }, [brokersQ.data]);

  const matches = (r: FIXPos) => (!firmFilter || r.firm_id.toLowerCase().includes(firmFilter.trim().toLowerCase())) && (!clientFilter || r.client_code.toLowerCase().includes(clientFilter.trim().toLowerCase()));
  const filteredInstruments = instruments.filter(matches);
  const filteredCash = cash.filter(matches);
  const instrumentColumns: TableColumn<FIXPos>[] = [
    { id: "firm", header: "Фирма", value: r => r.firm_id, className: "font-mono" },
    { id: "client", header: "Клиент", value: r => r.client_code, className: "font-mono" },
    { id: "account", header: "Счёт", value: r => r.account_code || r.keys?.ACC, className: "font-mono" },
    { id: "symbol", header: "Инструмент", value: r => r.symbol || r.security_id || r.keys?.SEC, cell: r => <><strong className="font-medium">{r.symbol || r.security_id || r.keys?.SEC || "—"}</strong><div className="text-[10px] text-muted">{[r.board, r.currency, r.keys?.KIND].filter(Boolean).join(" · ")}</div></> },
    { id: "balance", header: "Баланс", value: r => numberValue(item(r, "CUR_BAL") || item(r, "OPEN_BAL") || r.net_qty), cell: r => fmt(item(r, "CUR_BAL") || item(r, "OPEN_BAL") || r.net_qty, 4), numeric: true },
    { id: "long", header: "Long", value: r => numberValue(item(r, "CUR_LONG_POS") || r.long_qty), cell: r => <span className="text-emerald-700">{fmt(item(r, "CUR_LONG_POS") || r.long_qty, 4)}</span>, numeric: true },
    { id: "short", header: "Short", value: r => numberValue(item(r, "CUR_SHORT_POS") || r.short_qty), cell: r => <span className="text-rose-700">{fmt(item(r, "CUR_SHORT_POS") || r.short_qty, 4)}</span>, numeric: true },
    { id: "net", header: "Нетто", value: r => numberValue(item(r, "CUR_NET_POS") || r.net_qty), cell: r => <strong className={netTone(item(r, "CUR_NET_POS") || r.net_qty)}>{fmt(item(r, "CUR_NET_POS") || r.net_qty, 4)}</strong>, numeric: true },
    { id: "type", header: "Тип", value: r => r.pos_type, cell: r => posTypeLabel(r.pos_type) },
    { id: "price", header: "Цена", value: r => numberValue(item(r, "AVG_POS_PRICE") || item(r, "EFFECT_POS_PRICE") || r.settl_price), cell: r => fmt(item(r, "AVG_POS_PRICE") || item(r, "EFFECT_POS_PRICE") || r.settl_price), numeric: true },
    { id: "date", header: "Дата", value: r => r.clearing_date, className: "font-mono text-[10px]" },
  ];
  const cashColumns: TableColumn<FIXPos>[] = [
    { id: "firm", header: "Фирма", value: r => r.firm_id, className: "font-mono" },
    { id: "client", header: "Клиент", value: r => r.client_code, className: "font-mono" },
    { id: "currency", header: "Валюта", value: r => r.currency || r.keys?.CUR },
    { id: "tag", header: "TAG", value: r => r.keys?.TAG || r.symbol, className: "font-mono" },
    { id: "balance", header: "Текущий баланс", value: r => numberValue(item(r, "CUR_BAL") || r.pos_amt), cell: r => <strong className={netTone(item(r, "CUR_BAL") || r.pos_amt)}>{fmt(item(r, "CUR_BAL") || r.pos_amt)}</strong>, numeric: true },
    ...[ ["OPEN_BAL", "Входящий баланс"], ["CUR_LIM", "Лимит"], ["LOCKED", "Заблокировано"] ].map(([key, header]) => ({ id: key, header, value: (r: FIXPos) => numberValue(item(r, key)), cell: (r: FIXPos) => fmt(item(r, key)), numeric: true })),
    { id: "kind", header: "Вид", value: r => r.keys?.KIND },
    { id: "date", header: "Дата", value: r => r.clearing_date, className: "font-mono text-[10px]" },
  ];

  return (
    <div className="space-y-4">
      <PageHeader
        title="Позиции"
        subtitle="Инструменты и денежные остатки · автоматический поток из QUIK"
        actions={
          <div className="flex flex-wrap gap-2 text-xs">
            <div className={`status-badge ${subscription?.status === "ACTIVE" ? "badge-success" : "badge-warn"}`} title="Сервер поддерживает получение позиций независимо от открытой страницы"><i aria-hidden="true" />{subscription?.status === "ACTIVE" && subscription.mode === "snapshot" ? "Автообновление активно" : subscriptionLabels[subscription?.status || "STARTING"] || "Автоматическое получение позиций"}</div>
            <Pill label="Инструменты" value={filteredInstruments.length} />
            <Pill label="Денежные" value={filteredCash.length} />
            {positions.data?.source && <Pill label="Источник" value={positions.data.source} />}
            {snapshot && <Pill label={positions.data?.live ? "Данные" : "Сохранённый снимок"} value={new Date(snapshot.completed_at || snapshot.requested_at).toLocaleString("ru-RU")} />}
          </div>
        }
      />

      <div className="card space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex rounded-lg border border-line bg-surface p-0.5">
            <button
              type="button"
              className={`rounded-md px-3 py-1.5 text-sm font-medium ${
                tab === "instruments" ? "bg-panel text-accent shadow-sm" : "text-muted"
              }`}
              onClick={() => setTab("instruments")}
            >
              По инструментам (DL/FP)
            </button>
            <button
              type="button"
              className={`rounded-md px-3 py-1.5 text-sm font-medium ${
                tab === "cash" ? "bg-panel text-accent shadow-sm" : "text-muted"
              }`}
              onClick={() => setTab("cash")}
            >
              По деньгам (ML)
            </button>
          </div>
          <select
            className="input w-auto max-w-[200px]" aria-label="Брокер"
            value={brokerFilter}
            onChange={(e) => setBrokerFilter(e.target.value)}
          >
            <option value="">Все брокеры</option>
            {brokers.map(([id, name]) => (
              <option key={id} value={id}>
                {name}
              </option>
            ))}
          </select>
          <input
            className="input w-36"
            aria-label="Фирма" placeholder="Фирма"
            value={firmFilter}
            onChange={(e) => setFirmFilter(e.target.value)}
          />
          <input
            className="input w-36"
            aria-label="Код клиента" placeholder="Код клиента"
            value={clientFilter}
            onChange={(e) => setClientFilter(e.target.value)}
          />
        </div>
        <div className="text-xs text-muted">Получение позиций и восстановление потока выполняются автоматически. Фильтры меняют только отображение данных.</div>

        {positions.data?.note && (
          <div className="rounded-lg border border-amber-100 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            {positions.data.note}
          </div>
        )}
        {subscription?.error && <div role="status" className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900"><strong>Автоматическое восстановление позиций.</strong> {subscription.error}{subscription.next_retry_at && ` · следующая проверка ${new Date(subscription.next_retry_at).toLocaleTimeString("ru-RU")}`}</div>}
        {snapshot && subscription && !positions.data?.live && <div role="status" className="text-xs text-muted">Показан последний сохранённый снимок. После получения полного снимка QUIK таблица обновится автоматически.</div>}
        {positions.error && <ErrorBanner error={positions.error} />}
        <DataTable key={tab} title={tab === "instruments" ? "Позиции по инструментам" : "Денежные позиции"} rows={tab === "instruments" ? filteredInstruments : filteredCash} columns={tab === "instruments" ? instrumentColumns : cashColumns} rowKey={r => [r.adapter_id, r.pos_maint_rpt_id, r.firm_id, r.client_code, r.account_code, r.symbol, r.security_id, r.currency, r.pos_type, JSON.stringify(r.keys)].join("|")} loading={positions.isLoading} resetKey={brokerFilter + ":" + firmFilter + ":" + clientFilter} exportName={tab === "instruments" ? "positions" : "cash-positions"} getSearchText={r => [r.firm_id, r.client_code, r.account_code, r.symbol, r.security_id, r.board, r.currency, r.pos_type, JSON.stringify(r.keys), JSON.stringify(r.items)].join(" ")} emptyTitle={subscription?.status === "ACTIVE" ? "В этой выборке позиций нет" : "Ожидание позиций"} emptyHint="Шлюз автоматически получает позиции и денежные остатки из QUIK" summary={rows => [{ label: "Позиций", value: rows.length }, { label: "Клиентов", value: new Set(rows.map(r => r.firm_id + ":" + r.client_code)).size }, { label: "Фирм", value: new Set(rows.map(r => r.firm_id)).size }, { label: "Валют", value: new Set(rows.map(r => r.currency || r.keys?.CUR).filter(Boolean)).size }]} />

        {snapshot && (
          <div className="text-xs text-muted">
            Источник: FIX · адаптер {snapshot.adapter_id} · PosReqID {snapshot.pos_req_id} · отчётов{" "}
            {snapshot.report_count}
            {brokerFilter && (
              <>
                {" "}
                · фильтр брокера{" "}
                <Link className="text-accent" to={`/brokers/${brokerFilter}`}>
                  #{brokerFilter}
                </Link>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function Pill({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-lg border border-line bg-panel px-3 py-1.5 text-ink">
      <span className="mr-2 text-slate-500">{label}</span>
      <span className="font-mono font-semibold">{value}</span>
    </div>
  );
}
