import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";

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
};

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
  const qc = useQueryClient();
  const { connected } = useAdminStream();
  const [brokerFilter, setBrokerFilter] = useState("");
  const [firmFilter, setFirmFilter] = useState("");
  const [clientFilter, setClientFilter] = useState("");
  const [q, setQ] = useState("");
  const [tab, setTab] = useState<"instruments" | "cash">("instruments");

  const brokersQ = useQuery({
    queryKey: ["brokers"],
    queryFn: () => api<{ brokers: Array<{ id: number; name: string; code: string }> }>("/api/v1/admin/brokers"),
  });

  const positions = useQuery({
    queryKey: ["admin-positions", brokerFilter],
    queryFn: () => {
      const qs = brokerFilter ? `?broker_id=${encodeURIComponent(brokerFilter)}` : "";
      return api<PositionsResp>(`/api/v1/admin/positions${qs}`);
    },
    refetchInterval: liveRefetchInterval(connected, 10000),
  });

  const sync = useMutation({
    mutationFn: () =>
      api<{ summary?: string; received?: number; adapter_id?: string }>("/api/v1/admin/positions/sync", {
        method: "POST",
        body: JSON.stringify({
          broker_id: brokerFilter ? Number(brokerFilter) : undefined,
          firm_id: firmFilter || undefined,
          client_code: clientFilter || undefined,
        }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-positions"] });
    },
  });

  const instruments = positions.data?.instruments || [];
  const cash = positions.data?.cash || [];
  const snapshot = positions.data?.snapshot;

  const brokers = useMemo(() => {
    return (brokersQ.data?.brokers || [])
      .map((b) => [String(b.id), `${b.name} (${b.code})`] as const)
      .sort((a, b) => a[1].localeCompare(b[1], "ru"));
  }, [brokersQ.data]);

  const filteredInstruments = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return instruments;
    return instruments.filter((r) =>
      [r.firm_id, r.client_code, r.account_code, r.symbol, r.security_id, r.board, r.currency, r.pos_type, JSON.stringify(r.keys || {})]
        .join(" ")
        .toLowerCase()
        .includes(needle),
    );
  }, [instruments, q]);

  const filteredCash = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return cash;
    return cash.filter((r) =>
      [r.firm_id, r.client_code, r.currency, r.symbol, r.keys?.TAG, r.keys?.KIND, item(r, "CUR_BAL")]
        .join(" ")
        .toLowerCase()
        .includes(needle),
    );
  }, [cash, q]);

  return (
    <div className="space-y-4">
      <PageHeader
        title="Позиции"
        subtitle="Снимок позиций по FIX Request For Positions (AN→AP)"
        actions={
          <div className="flex flex-wrap gap-2 text-xs">
            <Pill label="Инструменты" value={filteredInstruments.length} />
            <Pill label="Денежные" value={filteredCash.length} />
            {positions.data?.source && <Pill label="Источник" value={positions.data.source} />}
            {snapshot && <Pill label="Снимок" value={new Date(snapshot.requested_at).toLocaleString("ru-RU")} />}
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
          <input
            className="input min-w-[180px] flex-1"
            placeholder="Поиск…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <select
            className="input w-auto max-w-[200px]"
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
            placeholder="Firm (role 13)"
            value={firmFilter}
            onChange={(e) => setFirmFilter(e.target.value)}
          />
          <input
            className="input w-36"
            placeholder="Client (role 3)"
            value={clientFilter}
            onChange={(e) => setClientFilter(e.target.value)}
          />
          <button className="btn-muted" onClick={() => void positions.refetch()}>
            Обновить
          </button>
          <button className="btn" disabled={sync.isPending} onClick={() => sync.mutate()}>
            {sync.isPending ? "Запрос…" : "Запросить по FIX"}
          </button>
        </div>

        {positions.data?.note && (
          <div className="rounded-lg border border-amber-100 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            {positions.data.note}
          </div>
        )}
        {sync.isError && (
          <div className="text-sm text-danger">{(sync.error as Error).message}</div>
        )}
        {sync.isSuccess && (
          <div className="text-sm text-emerald-700">
            {sync.data?.summary ||
              `Получено позиций: ${sync.data?.received ?? 0}`}
            {sync.data?.adapter_id ? ` · адаптер ${sync.data.adapter_id}` : ""}
            <span className="ml-2 text-slate-500">
              (каждый AP = одна позиция: ML деньги / DL инструмент)
            </span>
          </div>
        )}

        {positions.isLoading && <div className="py-8 text-center text-slate-400">Загрузка…</div>}

        {tab === "instruments" && !positions.isLoading && (
          <div className="overflow-x-auto rounded-lg border border-slate-200">
            <table className="w-full min-w-[1100px] text-left text-sm">
              <thead className="bg-slate-50">
                <tr>
                  {["Фирма", "Клиент", "Счёт", "Инструмент", "Баланс", "Long", "Short", "Нетто", "Тип", "Цена", "Дата"].map(
                    (h) => (
                      <th
                        key={h}
                        className="border-b border-line px-3 py-2.5 text-[12px] font-medium text-muted"
                      >
                        {h}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody>
                {filteredInstruments.length === 0 && (
                  <tr>
                    <td colSpan={11} className="px-3 py-10 text-center text-slate-400">
                      Нет позиций по инструментам — запросите по FIX
                    </td>
                  </tr>
                )}
                {filteredInstruments.map((r, i) => {
                  const bal = item(r, "CUR_BAL") || item(r, "OPEN_BAL") || String(r.net_qty);
                  const avg = item(r, "AVG_POS_PRICE") || item(r, "EFFECT_POS_PRICE") || String(r.settl_price ?? "");
                  return (
                    <tr key={`${r.firm_id}-${r.symbol}-${r.client_code}-${i}`} className="border-b border-slate-100 hover:bg-slate-50">
                      <td className="px-3 py-2 font-mono text-xs">{r.firm_id || "—"}</td>
                      <td className="px-3 py-2 font-mono text-xs">{r.client_code || "—"}</td>
                      <td className="px-3 py-2 font-mono text-xs">{r.account_code || r.keys?.ACC || "—"}</td>
                      <td className="px-3 py-2">
                        <div className="font-mono text-xs">{r.symbol || r.security_id || r.keys?.SEC || "—"}</div>
                        <div className="text-[11px] text-slate-400">
                          {[r.keys?.KIND, r.keys?.STLDATE, r.currency].filter(Boolean).join(" · ") || "—"}
                        </div>
                      </td>
                      <td className={`px-3 py-2 font-mono tabular-nums ${netTone(bal)}`}>{fmt(bal, 4)}</td>
                      <td className="px-3 py-2 font-mono tabular-nums text-emerald-700">{fmt(item(r, "CUR_LONG_POS") || r.long_qty, 4)}</td>
                      <td className="px-3 py-2 font-mono tabular-nums text-rose-700">{fmt(item(r, "CUR_SHORT_POS") || r.short_qty, 4)}</td>
                      <td className={`px-3 py-2 font-mono font-semibold tabular-nums ${netTone(item(r, "CUR_NET_POS") || r.net_qty)}`}>
                        {fmt(item(r, "CUR_NET_POS") || r.net_qty, 4)}
                      </td>
                      <td className="px-3 py-2 font-mono text-[11px] text-slate-500">{posTypeLabel(r.pos_type)}</td>
                      <td className="px-3 py-2 font-mono tabular-nums">{fmt(avg)}</td>
                      <td className="px-3 py-2 font-mono text-[11px] text-slate-500">{r.clearing_date || "—"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {tab === "cash" && !positions.isLoading && (
          <div className="overflow-x-auto rounded-lg border border-slate-200">
            <table className="w-full min-w-[1000px] text-left text-sm">
              <thead className="bg-slate-50">
                <tr>
                  {["Фирма", "Клиент", "Валюта", "TAG", "CUR_BAL", "OPEN_BAL", "CUR_LIM", "LOCKED", "KIND", "Дата"].map((h) => (
                    <th
                      key={h}
                      className="border-b border-line px-3 py-2.5 text-[12px] font-medium text-muted"
                    >
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {filteredCash.length === 0 && (
                  <tr>
                    <td colSpan={10} className="px-3 py-10 text-center text-slate-400">
                      Нет денежных позиций — запросите по FIX
                    </td>
                  </tr>
                )}
                {filteredCash.map((r, i) => (
                  <tr key={`${r.firm_id}-${r.currency}-${r.keys?.TAG}-${i}`} className="border-b border-slate-100 hover:bg-slate-50">
                    <td className="px-3 py-2 font-mono text-xs">{r.firm_id || "—"}</td>
                    <td className="px-3 py-2 font-mono text-xs">{r.client_code || "—"}</td>
                    <td className="px-3 py-2 font-mono text-xs">{r.currency || r.keys?.CUR || "—"}</td>
                    <td className="px-3 py-2 font-mono text-xs">{r.keys?.TAG || r.symbol || "—"}</td>
                    <td className={`px-3 py-2 font-mono font-semibold tabular-nums ${netTone(item(r, "CUR_BAL") || r.pos_amt)}`}>
                      {fmt(item(r, "CUR_BAL") || r.pos_amt)}
                    </td>
                    <td className="px-3 py-2 font-mono tabular-nums">{fmt(item(r, "OPEN_BAL"))}</td>
                    <td className="px-3 py-2 font-mono tabular-nums">{fmt(item(r, "CUR_LIM"))}</td>
                    <td className="px-3 py-2 font-mono tabular-nums">{fmt(item(r, "LOCKED"))}</td>
                    <td className="px-3 py-2 font-mono text-[11px] text-slate-500">
                      {[r.keys?.KIND, r.keys?.STLDATE].filter(Boolean).join(" · ") || "—"}
                    </td>
                    <td className="px-3 py-2 font-mono text-[11px] text-slate-500">{r.clearing_date || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {snapshot && (
          <div className="text-xs text-slate-400">
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
