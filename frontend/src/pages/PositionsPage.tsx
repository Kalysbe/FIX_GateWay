import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { rawSearchText } from "../components/rawColumns";
import { quikColumns, type QuikColumn } from "../components/quikColumns";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { DataTable } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";

type PositionsResp = { cash: DBOrder[]; instruments: DBOrder[] };

const text = (row: DBOrder, field: string) => dbValue(row[field]).trim().toLowerCase();

// Balances of a DepoLimits row; limits, values and the load date are in the
// column menu.
const DEPO_COLUMNS: QuikColumn[] = [
  { id: "firm", header: "Фирма", field: "FirmId" },
  { id: "client", header: "Клиент / счёт", field: "ClientCode", sub: "Trdacc" },
  { id: "sec", header: "Инструмент", field: "SecCode" },
  { id: "kind", header: "Срок", field: "LimitKind" },
  { id: "open", header: "Входящий", field: "OpenBal", kind: "number" },
  { id: "current", header: "Текущий", field: "CurrentBal", kind: "number" },
  { id: "lockedBuy", header: "Блок. покупка", field: "LockedBuy", kind: "number" },
  { id: "lockedSell", header: "Блок. продажа", field: "LockedSell", kind: "number" },
  { id: "price", header: "Ср. цена", field: "AwgPositionPrice", kind: "number", frac: 2, sub: "WAPositionPriceCurrency" },
];

// Balances of a MoneyLimits row.
const MONEY_COLUMNS: QuikColumn[] = [
  { id: "firm", header: "Фирма", field: "FirmId" },
  { id: "client", header: "Клиент / тег", field: "ClientCode", sub: "Tag" },
  { id: "currency", header: "Валюта", field: "CurrCode" },
  { id: "kind", header: "Срок", field: "LimitKind" },
  { id: "open", header: "Входящий", field: "OpenBal", kind: "number", frac: 2 },
  { id: "current", header: "Текущий", field: "CurrentBal", kind: "number", frac: 2 },
  { id: "locked", header: "Заблокировано", field: "Locked", kind: "number", frac: 2 },
  { id: "limit", header: "Текущий лимит", field: "CurrentLimit", kind: "number", frac: 2 },
];

// A row is empty when every balance is zero or null; hiding it is display only.
const BALANCES = { instruments: ["OpenBal", "CurrentBal", "LockedBuy", "LockedSell"], cash: ["OpenBal", "CurrentBal", "Locked", "CurrentLimit"] };
const isZero = (v: DBOrder[string]) => v == null || /^-?0*(\.0*)?$/.test(String(v).trim());

// Current rows of QUIK "DepoLimits" and "MoneyLimits" as stored; the Gateway
// keeps no copy of them.
export default function PositionsPage() {
  const { connected } = useAdminStream();
  const [brokerFilter, setBrokerFilter] = useState("");
  const [firmFilter, setFirmFilter] = useState("");
  const [clientFilter, setClientFilter] = useState("");
  const [tab, setTab] = useState<"instruments" | "cash">("instruments");
  const [hideZero, setHideZero] = useState(true);

  const brokersQ = useQuery({
    queryKey: ["brokers"],
    queryFn: () => api<{ brokers: Array<{ id: number; name: string; code: string }> }>("/api/v1/admin/brokers"),
  });

  const positions = useQuery({
    queryKey: ["admin-positions", brokerFilter],
    queryFn: async ({ signal }) => {
      const qs = brokerFilter ? `?broker_id=${encodeURIComponent(brokerFilter)}` : "";
      return (await apiWithHeaders<PositionsResp>(`/api/v1/admin/positions${qs}`, { signal }, parseDBJSON)).data;
    },
    refetchInterval: liveRefetchInterval(connected, 2000, 5000),
  });

  const brokers = useMemo(() => {
    return (brokersQ.data?.brokers || [])
      .map((b) => [String(b.id), `${b.name} (${b.code})`] as const)
      .sort((a, b) => a[1].localeCompare(b[1], "ru"));
  }, [brokersQ.data]);

  const matches = (balances: string[]) => (r: DBOrder) => text(r, "FirmId").includes(firmFilter.trim().toLowerCase()) && text(r, "ClientCode").includes(clientFilter.trim().toLowerCase())
    && (!hideZero || balances.some(f => !isZero(r[f])));
  const allInstruments = positions.data?.instruments ?? [];
  const allCash = positions.data?.cash ?? [];
  const instruments = allInstruments.filter(matches(BALANCES.instruments));
  const cash = allCash.filter(matches(BALANCES.cash));
  const rows = tab === "instruments" ? instruments : cash;
  // Columns from every loaded row, so filters never drop a column.
  const columns = useMemo(() => tab === "instruments" ? quikColumns(allInstruments, DEPO_COLUMNS) : quikColumns(allCash, MONEY_COLUMNS), [tab, allInstruments, allCash]);
  const rowKey = (r: DBOrder) => ["LoadDate", "FirmId", "ClientCode", "SecCode", "Trdacc", "Tag", "CurrCode", "LimitKind"].map(f => dbValue(r[f])).join("\u0000");

  return (
    <div className="raw-orders space-y-4">
      <PageHeader title="Позиции" subtitle="Таблицы QUIK DepoLimits и MoneyLimits за последнюю дату. Остальные поля — в меню столбцов" />

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
              Инструменты ({instruments.length})
            </button>
            <button
              type="button"
              className={`rounded-md px-3 py-1.5 text-sm font-medium ${
                tab === "cash" ? "bg-panel text-accent shadow-sm" : "text-muted"
              }`}
              onClick={() => setTab("cash")}
            >
              Деньги ({cash.length})
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
          <label className="flex items-center gap-2 text-sm text-muted">
            <input type="checkbox" checked={hideZero} onChange={(e) => setHideZero(e.target.checked)} />
            Скрыть нулевые остатки
          </label>
        </div>

        {positions.error && <ErrorBanner error={positions.error} onRetry={() => positions.refetch()} />}
        <DataTable key={tab} title={tab === "instruments" ? "Позиции по инструментам" : "Денежные позиции"} rows={positions.error ? [] : rows} columns={columns} rowKey={rowKey} getSearchText={rawSearchText(rows)} loading={positions.isLoading} resetKey={brokerFilter + ":" + firmFilter + ":" + clientFilter + ":" + hideZero} exportName={tab === "instruments" ? "positions" : "cash-positions"} showTotals={false} emptyTitle={positions.error ? "Позиции временно недоступны" : "В этой выборке позиций нет"} emptyHint={hideZero && (tab === "instruments" ? allInstruments : allCash).length ? "Нулевые остатки скрыты" : undefined} />
      </div>
    </div>
  );
}
