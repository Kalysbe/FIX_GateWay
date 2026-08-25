import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, sideLabel } from "../labels";

type Trade = {
  id: string;
  order_id: string;
  exec_id: string;
  last_qty: string | number;
  last_px: string | number;
  cum_qty: string | number;
  leaves_qty: string | number;
  adapter_id?: string | null;
  created_at: string;
  client_order_id?: string;
  symbol?: string;
  board?: string;
  side?: string;
  client_code?: string;
  quik_firm?: string;
  firm_name?: string;
  fix_cl_ord_id?: string | null;
  quik_order_id?: string | null;
};

type Deal = {
  key: string;
  label: string;
  trades: Trade[];
  paired: boolean;
  createdAt: number;
  symbol: string;
  board: string;
  lastQty: string | number;
  lastPx: string | number;
};

/** KASE/QUIK: LISTEDSTOCKK-4g-B-1-1-N ↔ LISTEDSTOCKK-4g-S-1-1-N */
function matchKeyFromExecId(execId: string): string {
  const id = String(execId || "").trim();
  if (!id) return "";
  const normalized = id.replace(/-([BS])-/i, "-*-");
  return normalized || id;
}

function dealLabelFromMatchKey(key: string): string {
  return key.replace("-*-", "-");
}

function SideBadge({ side }: { side?: string }) {
  const buy = (side || "").toUpperCase() === "BUY";
  return (
    <span
      className={`inline-flex rounded-md px-2 py-0.5 text-xs font-semibold ${
        buy ? "bg-emerald-50 text-emerald-700" : "bg-rose-50 text-rose-700"
      }`}
    >
      {sideLabel(side)}
    </span>
  );
}

function groupDeals(trades: Trade[]): Deal[] {
  const map = new Map<string, Trade[]>();
  for (const t of trades) {
    const key = matchKeyFromExecId(t.exec_id) || `solo:${t.id}`;
    const list = map.get(key) || [];
    list.push(t);
    map.set(key, list);
  }

  const deals: Deal[] = [];
  for (const [key, legs] of map) {
    const sorted = [...legs].sort((a, b) => {
      const sa = (a.side || "").toUpperCase() === "BUY" ? 0 : 1;
      const sb = (b.side || "").toUpperCase() === "BUY" ? 0 : 1;
      if (sa !== sb) return sa - sb;
      return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
    });
    const sides = new Set(sorted.map((t) => (t.side || "").toUpperCase()));
    const paired = sides.has("BUY") && sides.has("SELL") && sorted.length >= 2;
    const sample = sorted[0];
    deals.push({
      key,
      label: dealLabelFromMatchKey(key.startsWith("solo:") ? sample.exec_id : key),
      trades: sorted,
      paired,
      createdAt: Math.max(...sorted.map((t) => new Date(t.created_at).getTime())),
      symbol: sample.symbol || "",
      board: sample.board || "",
      lastQty: sample.last_qty,
      lastPx: sample.last_px,
    });
  }

  return deals.sort((a, b) => b.createdAt - a.createdAt);
}

function dealMatchesQuery(deal: Deal, q: string): boolean {
  if (!q) return true;
  const hay = deal.trades
    .flatMap((t) => [
      t.exec_id,
      t.client_order_id,
      t.order_id,
      t.firm_name,
      t.quik_firm,
      t.client_code,
      t.symbol,
      t.board,
      t.side,
      t.adapter_id,
      t.fix_cl_ord_id,
      t.quik_order_id,
      String(t.last_qty),
      String(t.last_px),
      deal.label,
      deal.key,
    ])
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return hay.includes(q);
}

function dealPassesFilters(
  deal: Deal,
  filters: { side: string; firm: string; symbol: string; adapter: string },
): boolean {
  return deal.trades.some((t) => {
    if (filters.side && t.side !== filters.side) return false;
    if (filters.firm) {
      const label = t.firm_name || t.quik_firm || "";
      if (label !== filters.firm) return false;
    }
    if (filters.symbol && t.symbol !== filters.symbol) return false;
    if (filters.adapter && t.adapter_id !== filters.adapter) return false;
    return true;
  });
}

export default function TradesPage() {
  const q = useQuery({
    queryKey: ["admin-trades"],
    queryFn: () => api<{ trades: Trade[] }>("/api/v1/admin/trades"),
    refetchInterval: 5000,
  });

  const data = q.data?.trades || [];
  const [globalFilter, setGlobalFilter] = useState("");
  const [sideFilter, setSideFilter] = useState("");
  const [firmFilter, setFirmFilter] = useState("");
  const [symbolFilter, setSymbolFilter] = useState("");
  const [adapterFilter, setAdapterFilter] = useState("");
  const [pageIndex, setPageIndex] = useState(0);
  const [pageSize, setPageSize] = useState(20);

  const allDeals = useMemo(() => groupDeals(data), [data]);

  const firmOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      const label = t.firm_name || t.quik_firm;
      if (label) set.add(label);
    });
    return Array.from(set).sort((a, b) => a.localeCompare(b, "ru"));
  }, [data]);

  const symbolOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      if (t.symbol) set.add(t.symbol);
    });
    return Array.from(set).sort();
  }, [data]);

  const adapterOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      if (t.adapter_id) set.add(t.adapter_id);
    });
    return Array.from(set).sort();
  }, [data]);

  const filteredDeals = useMemo(() => {
    const query = globalFilter.trim().toLowerCase();
    const filters = {
      side: sideFilter,
      firm: firmFilter,
      symbol: symbolFilter,
      adapter: adapterFilter,
    };
    return allDeals.filter(
      (d) => dealMatchesQuery(d, query) && dealPassesFilters(d, filters),
    );
  }, [allDeals, globalFilter, sideFilter, firmFilter, symbolFilter, adapterFilter]);

  const pageCount = Math.max(1, Math.ceil(filteredDeals.length / pageSize));
  const safePage = Math.min(pageIndex, pageCount - 1);
  const pageDeals = filteredDeals.slice(safePage * pageSize, (safePage + 1) * pageSize);

  const totalLegs = data.length;

  function resetFilters() {
    setGlobalFilter("");
    setSideFilter("");
    setFirmFilter("");
    setSymbolFilter("");
    setAdapterFilter("");
    setPageIndex(0);
  }

  function exportCsv() {
    const rows = filteredDeals.flatMap((d) =>
      d.trades.map((t) => ({ ...t, deal_id: d.label, paired: d.paired ? "yes" : "no" })),
    );
    const header = [
      "deal_id",
      "paired",
      "created_at",
      "exec_id",
      "client_order_id",
      "firm_name",
      "quik_firm",
      "client_code",
      "symbol",
      "board",
      "side",
      "last_qty",
      "last_px",
      "cum_qty",
      "leaves_qty",
      "adapter_id",
    ];
    const lines = [
      header.join(","),
      ...rows.map((t) =>
        header
          .map((k) => {
            const v = String((t as Record<string, unknown>)[k] ?? "");
            return `"${v.replace(/"/g, '""')}"`;
          })
          .join(","),
      ),
    ];
    const blob = new Blob([lines.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `trades-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Сделки</h1>
          <p className="text-sm text-slate-500">
            FIX ExecutionReport · LastQty &gt; 0 · обновление каждые 5 с
          </p>
        </div>
        <div className="flex flex-wrap gap-2 text-xs">
          <StatPill label="Всего" value={totalLegs} />
          <StatPill label="Сделок" value={allDeals.length} />
          <StatPill label="После фильтра" value={filteredDeals.length} />
        </div>
      </div>

      <div className="card space-y-3 bg-white">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[240px] flex-1">
            <input
              className="input pl-9"
              placeholder="Поиск: ExecID, сделка, ордер, фирма, клиент…"
              value={globalFilter}
              onChange={(e) => {
                setGlobalFilter(e.target.value);
                setPageIndex(0);
              }}
            />
            <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400">⌕</span>
          </div>
          <select
            className="input w-auto"
            value={sideFilter}
            onChange={(e) => {
              setSideFilter(e.target.value);
              setPageIndex(0);
            }}
          >
            <option value="">Сторона: все</option>
            <option value="BUY">Покупка</option>
            <option value="SELL">Продажа</option>
          </select>
          <select
            className="input w-auto max-w-[180px]"
            value={firmFilter}
            onChange={(e) => {
              setFirmFilter(e.target.value);
              setPageIndex(0);
            }}
          >
            <option value="">Фирма: все</option>
            {firmOptions.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
          <select
            className="input w-auto max-w-[180px]"
            value={symbolFilter}
            onChange={(e) => {
              setSymbolFilter(e.target.value);
              setPageIndex(0);
            }}
          >
            <option value="">Инструмент: все</option>
            {symbolOptions.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <select
            className="input w-auto max-w-[160px]"
            value={adapterFilter}
            onChange={(e) => {
              setAdapterFilter(e.target.value);
              setPageIndex(0);
            }}
          >
            <option value="">Адаптер: все</option>
            {adapterOptions.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
          <button className="btn-muted" onClick={resetFilters}>
            Сбросить
          </button>
          <button className="btn-muted" onClick={() => void q.refetch()}>
            Обновить
          </button>
          <button className="btn" onClick={exportCsv} disabled={filteredDeals.length === 0}>
            CSV
          </button>
        </div>

        <div className="overflow-x-auto rounded-lg border border-slate-200">
          <table className="w-full min-w-[1100px] text-left text-sm">
            <thead className="sticky top-0 z-10 bg-slate-50">
              <tr className="border-b border-slate-200">
                {[
                  "Время",
                  "ExecID",
                  "Ордер",
                  "Фирма",
                  "Клиент",
                  "Инструмент",
                  "Сторона",
                  "Кол-во",
                  "Цена",
                  "Накоплено",
                  "Остаток",
                  "Адаптер",
                ].map((h) => (
                  <th
                    key={h}
                    className="border-b border-slate-200 px-3 py-2.5 text-xs font-semibold uppercase tracking-wide text-slate-500"
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {q.isLoading && (
                <tr>
                  <td colSpan={12} className="px-3 py-10 text-center text-slate-500">
                    Загрузка сделок…
                  </td>
                </tr>
              )}
              {!q.isLoading && pageDeals.length === 0 && (
                <tr>
                  <td colSpan={12} className="px-3 py-10 text-center text-slate-500">
                    {totalLegs === 0
                      ? "Нет сделок — появятся после FILL/PARTIAL из FIX"
                      : "Ничего не найдено по текущим фильтрам"}
                  </td>
                </tr>
              )}
              {pageDeals.map((deal, dealIdx) => {
                // paired buy/sell share one gray block; alternate shade between deals
                const bg =
                  dealIdx % 2 === 0 ? "bg-slate-100/90" : "bg-slate-50";

                return deal.trades.map((t, legIdx) => {
                  const isLast = legIdx === deal.trades.length - 1;
                  return (
                    <tr
                      key={t.id}
                      className={`${bg} transition-colors hover:bg-slate-200/60 ${
                        isLast ? "border-b border-slate-200" : "border-b border-slate-100/80"
                      }`}
                    >
                      <td className="px-3 py-2 align-middle">
                        <span className="whitespace-nowrap font-mono text-[11px] text-slate-600">
                          {formatDateTime(t.created_at)}
                        </span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span
                          className="block max-w-[160px] truncate font-mono text-[11px]"
                          title={t.exec_id}
                        >
                          {t.exec_id || "—"}
                        </span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <Link
                          className="font-mono text-xs text-accent hover:underline"
                          to={`/orders/${t.order_id}`}
                          title={t.client_order_id || t.order_id}
                        >
                          {t.client_order_id || String(t.order_id).slice(0, 8)}
                        </Link>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <div className="text-sm">{t.firm_name || "—"}</div>
                        {t.quik_firm ? (
                          <div className="font-mono text-[11px] text-slate-500">{t.quik_firm}</div>
                        ) : null}
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs">{t.client_code || "—"}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <div className="font-mono text-xs">{t.symbol || "—"}</div>
                        {t.board ? <div className="text-[11px] text-slate-500">{t.board}</div> : null}
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <SideBadge side={t.side} />
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-sm tabular-nums">{t.last_qty}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-sm tabular-nums">{t.last_px}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs tabular-nums text-slate-600">{t.cum_qty}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="font-mono text-xs tabular-nums text-slate-600">{t.leaves_qty}</span>
                      </td>
                      <td className="px-3 py-2 align-middle">
                        <span className="rounded bg-white/70 px-1.5 py-0.5 font-mono text-[11px] text-slate-600">
                          {t.adapter_id || "—"}
                        </span>
                      </td>
                    </tr>
                  );
                });
              })}
            </tbody>
          </table>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
          <div className="text-slate-500">
            Показано{" "}
            <span className="font-mono text-ink">
              {filteredDeals.length === 0 ? 0 : safePage * pageSize + 1}–
              {Math.min((safePage + 1) * pageSize, filteredDeals.length)}
            </span>{" "}
            из <span className="font-mono text-ink">{filteredDeals.length}</span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2 text-slate-500">
              На странице
              <select
                className="input w-auto py-1"
                value={pageSize}
                onChange={(e) => {
                  setPageSize(Number(e.target.value));
                  setPageIndex(0);
                }}
              >
                {[10, 25, 50, 100].map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="btn-muted px-2 py-1"
              disabled={safePage <= 0}
              onClick={() => setPageIndex(0)}
            >
              «
            </button>
            <button
              className="btn-muted px-2 py-1"
              disabled={safePage <= 0}
              onClick={() => setPageIndex((p) => Math.max(0, p - 1))}
            >
              ‹
            </button>
            <span className="min-w-[7rem] text-center font-mono text-xs text-slate-600">
              {safePage + 1} / {pageCount}
            </span>
            <button
              className="btn-muted px-2 py-1"
              disabled={safePage >= pageCount - 1}
              onClick={() => setPageIndex((p) => Math.min(pageCount - 1, p + 1))}
            >
              ›
            </button>
            <button
              className="btn-muted px-2 py-1"
              disabled={safePage >= pageCount - 1}
              onClick={() => setPageIndex(pageCount - 1)}
            >
              »
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function StatPill({
  label,
  value,
}: {
  label: string;
  value: number;
}) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-ink">
      <span className="mr-2 text-slate-500">{label}</span>
      <span className="font-mono font-semibold">{value}</span>
    </div>
  );
}
