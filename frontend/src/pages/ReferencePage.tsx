import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, apiWithHeaders } from "../api";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { rawColumns, rawSearchText } from "../components/rawColumns";
import { StatusBadge } from "../components/StatusBadge";
import { DataTable, type TableColumn } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";

type Instrument = {
  id: number;
  internal_symbol: string;
  quik_symbol: string;
  board: string;
  currency: string;
  security_type: string;
  status: string;
};


export default function ReferencePage() {
  const qc = useQueryClient();
  const [tab, setTab] = useState<"instruments" | "firms">("instruments");

  const instruments = useQuery({
    queryKey: ["admin-instruments"],
    queryFn: () => api<{ instruments: Instrument[]; count: number }>("/api/v1/admin/instruments"),
  });
  const firms = useQuery({
    queryKey: ["admin-firms"],
    // Rows of QUIK "Firms" as stored.
    queryFn: async ({ signal }) => (await apiWithHeaders<DBOrder[]>("/api/v1/admin/firms", { signal }, parseDBJSON)).data ?? [],
  });

  const sync = useMutation({
    mutationFn: () =>
      api<{
        result: {
          instruments_upserted: number;
          instruments_removed: number;
          instruments_inactive: number;
          adapter_id?: string;
        };
      }>("/api/v1/admin/instruments/sync", { method: "POST" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-instruments"] });
    },
  });

  const instrumentColumns: TableColumn<Instrument>[] = [
    { id: "id", header: "ID", value: i => i.id, numeric: true },
    { id: "symbol", header: "Инструмент", value: i => i.quik_symbol, className: "font-mono font-medium" },
    { id: "board", header: "Борд / класс", value: i => i.board, className: "font-mono" },
    { id: "currency", header: "Валюта", value: i => i.currency },
    { id: "type", header: "Тип", value: i => i.security_type },
    { id: "status", header: "Статус", value: i => i.status, cell: i => <StatusBadge status={i.status} /> },
    { id: "internal", header: "Внутренний код", value: i => i.internal_symbol, className: "font-mono text-[10px]" },
  ];
  const firmRows = firms.data ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Справочник"
        subtitle="Инструменты, торговые классы и фирмы · синхронизация с QUIK"
        actions={
          <button className="btn" disabled={sync.isPending} onClick={() => sync.mutate()}>
            {sync.isPending ? "Запрос по FIX…" : "Синхронизировать по FIX"}
          </button>
        }
      />

      {sync.isSuccess && (
        <div className="text-sm text-accent">
          Синхронизировано: {sync.data.result.instruments_upserted} активных
          {sync.data.result.instruments_removed
            ? `, удалено устаревших ${sync.data.result.instruments_removed}`
            : ""}
          {sync.data.result.instruments_inactive
            ? `, деактивировано (есть заявки) ${sync.data.result.instruments_inactive}`
            : ""}
          {sync.data.result.adapter_id ? ` · ${sync.data.result.adapter_id}` : ""}
        </div>
      )}
      {sync.error && <div className="text-danger text-sm">{(sync.error as Error).message}</div>}

      <div className="flex flex-wrap items-center gap-2">
        <button
          className={tab === "instruments" ? "btn" : "btn-muted"}
          onClick={() => setTab("instruments")}
        >
          Инструменты ({instruments.data?.count ?? "…"})
        </button>
        <button className={tab === "firms" ? "btn" : "btn-muted"} onClick={() => setTab("firms")}>
          Фирмы ({firms.data ? firmRows.length : "…"})
        </button>
      </div>

      {tab === "instruments" ? <>
        {instruments.error && <ErrorBanner error={instruments.error} onRetry={() => instruments.refetch()} />}
        <DataTable title="Инструменты" rows={instruments.data?.instruments ?? []} columns={instrumentColumns} rowKey={i => i.id} loading={instruments.isLoading} exportName="instruments" emptyHint="Синхронизируйте справочник по FIX" summary={rows => [{ label: "Инструментов", value: rows.length }, { label: "Классов", value: new Set(rows.map(i => i.board)).size }, { label: "Активны", value: rows.filter(i => i.status === "ACTIVE").length, tone: "success" }]} />
      </> : <>
        <p className="text-xs text-muted">Строки таблицы QUIK Firms в том виде, в каком они хранятся в базе</p>
        {firms.error && <ErrorBanner error={firms.error} onRetry={() => firms.refetch()} />}
        <DataTable title="Фирмы" rows={firmRows} columns={rawColumns(firmRows)} rowKey={f => `${dbValue(f.FirmId)}\u0000${dbValue(f.Exchange)}`} getSearchText={rawSearchText(firmRows)} loading={firms.isLoading} exportName="firms" showTotals={false} emptyTitle={firms.error ? "Таблица Firms недоступна" : "В Firms нет строк"} />
      </>}
    </div>
  );
}
