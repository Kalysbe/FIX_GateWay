import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { api } from "../api";
import { statusLabel } from "../labels";

type Instrument = {
  id: number;
  internal_symbol: string;
  quik_symbol: string;
  board: string;
  currency: string;
  security_type: string;
  status: string;
};

type Firm = {
  firm_id: string;
  exchange: string;
  firm_name: string;
  status: string;
  status_flag?: number;
};

export default function ReferencePage() {
  const qc = useQueryClient();
  const [tab, setTab] = useState<"instruments" | "firms">("instruments");
  const [q, setQ] = useState("");

  const instruments = useQuery({
    queryKey: ["admin-instruments"],
    queryFn: () => api<{ instruments: Instrument[]; count: number }>("/api/v1/admin/instruments"),
  });
  const firms = useQuery({
    queryKey: ["admin-firms"],
    queryFn: () => api<{ firms: Firm[]; count: number; note?: string }>("/api/v1/admin/firms"),
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

  const filteredInstruments = useMemo(() => {
    const list = instruments.data?.instruments || [];
    const needle = q.trim().toLowerCase();
    if (!needle) return list;
    return list.filter(
      (i) =>
        i.quik_symbol.toLowerCase().includes(needle) ||
        i.board.toLowerCase().includes(needle) ||
        i.internal_symbol.toLowerCase().includes(needle) ||
        i.currency.toLowerCase().includes(needle),
    );
  }, [instruments.data, q]);

  const filteredFirms = useMemo(() => {
    const list = firms.data?.firms || [];
    const needle = q.trim().toLowerCase();
    if (!needle) return list;
    return list.filter(
      (f) =>
        f.firm_id.toLowerCase().includes(needle) ||
        f.firm_name.toLowerCase().includes(needle) ||
        f.exchange.toLowerCase().includes(needle) ||
        f.status.toLowerCase().includes(needle),
    );
  }, [firms.data, q]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Справочник</h1>
          <p className="text-sm text-slate-500">
            Инструменты синхронизируются по FIX Security Definition (`35=c` → `35=d`)
          </p>
        </div>
        <button className="btn" disabled={sync.isPending} onClick={() => sync.mutate()}>
          {sync.isPending ? "Запрос по FIX…" : "Синхронизировать по FIX"}
        </button>
      </div>

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
          Фирмы ({firms.data?.count ?? "…"})
        </button>
        <input
          className="input ml-auto max-w-xs"
          placeholder="Поиск…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>

      {tab === "instruments" && (
        <div className="card overflow-x-auto">
          {instruments.isLoading && <div className="text-sm text-slate-500">Загрузка…</div>}
          {instruments.error && (
            <div className="text-danger text-sm">{(instruments.error as Error).message}</div>
          )}
          {!instruments.isLoading && !instruments.error && (
            <table className="table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Символ</th>
                  <th>Борд / Класс</th>
                  <th>Валюта</th>
                  <th>Тип</th>
                  <th>Статус</th>
                  <th>Внутренний</th>
                </tr>
              </thead>
              <tbody>
                {filteredInstruments.map((i) => (
                  <tr key={i.id}>
                    <td>{i.id}</td>
                    <td className="font-mono">{i.quik_symbol}</td>
                    <td className="font-mono">{i.board}</td>
                    <td>{i.currency}</td>
                    <td>{i.security_type}</td>
                    <td className={`status-${i.status}`}>{statusLabel(i.status)}</td>
                    <td className="font-mono text-xs text-slate-500">{i.internal_symbol}</td>
                  </tr>
                ))}
                {filteredInstruments.length === 0 && (
                  <tr>
                    <td colSpan={7} className="text-slate-500">
                      Нет записей — нажмите «Синхронизировать по FIX»
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          )}
        </div>
      )}

      {tab === "firms" && (
        <div className="card overflow-x-auto">
          {firms.data?.note && <div className="mb-3 text-xs text-slate-500">{firms.data.note}</div>}
          {firms.isLoading && <div className="text-sm text-slate-500">Загрузка…</div>}
          {firms.error && <div className="text-danger text-sm">{(firms.error as Error).message}</div>}
          {!firms.isLoading && !firms.error && (
            <table className="table">
              <thead>
                <tr>
                  <th>ID фирмы</th>
                  <th>Название</th>
                  <th>Биржа</th>
                  <th>Статус</th>
                  <th>Флаг</th>
                </tr>
              </thead>
              <tbody>
                {filteredFirms.map((f) => (
                  <tr key={f.firm_id}>
                    <td className="font-mono">{f.firm_id}</td>
                    <td>{f.firm_name}</td>
                    <td className="font-mono">{f.exchange}</td>
                    <td>{statusLabel(f.status)}</td>
                    <td>{f.status_flag ?? "—"}</td>
                  </tr>
                ))}
                {filteredFirms.length === 0 && (
                  <tr>
                    <td colSpan={5} className="text-slate-500">
                      Нет записей (фирмы не приходят через FIX Security Definition)
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  );
}
