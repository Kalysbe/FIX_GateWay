import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, apiWithHeaders } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { rawColumns, rawSearchText } from "../components/rawColumns";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { DataTable } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";

type PositionsResp = { cash: DBOrder[]; instruments: DBOrder[] };

const text = (row: DBOrder, field: string) => dbValue(row[field]).trim().toLowerCase();

// Current rows of QUIK "DepoLimits" and "MoneyLimits" as stored; the Gateway
// keeps no copy of them.
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

  const matches = (r: DBOrder) => text(r, "FirmId").includes(firmFilter.trim().toLowerCase()) && text(r, "ClientCode").includes(clientFilter.trim().toLowerCase());
  const instruments = (positions.data?.instruments ?? []).filter(matches);
  const cash = (positions.data?.cash ?? []).filter(matches);
  const rows = tab === "instruments" ? instruments : cash;
  const rowKey = (r: DBOrder) => ["LoadDate", "FirmId", "ClientCode", "SecCode", "Trdacc", "Tag", "CurrCode", "LimitKind"].map(f => dbValue(r[f])).join("\u0000");

  return (
    <div className="space-y-4">
      <PageHeader title="Позиции" subtitle="Строки таблиц QUIK DepoLimits и MoneyLimits за последнюю дату в том виде, в каком они хранятся в базе" />

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
        </div>

        {positions.error && <ErrorBanner error={positions.error} onRetry={() => positions.refetch()} />}
        <DataTable key={tab} title={tab === "instruments" ? "Позиции по инструментам" : "Денежные позиции"} rows={positions.error ? [] : rows} columns={rawColumns(rows)} rowKey={rowKey} getSearchText={rawSearchText(rows)} loading={positions.isLoading} resetKey={brokerFilter + ":" + firmFilter + ":" + clientFilter} exportName={tab === "instruments" ? "positions" : "cash-positions"} showTotals={false} emptyTitle={positions.error ? "Позиции временно недоступны" : "В этой выборке позиций нет"} />
      </div>
    </div>
  );
}
