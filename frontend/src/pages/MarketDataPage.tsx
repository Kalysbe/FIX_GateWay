import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";

type BookLevel = {
  price?: string;
  size?: string;
};

type Quote = {
  adapter_id?: string;
  md_req_id?: string;
  symbol: string;
  security_id?: string;
  board: string;
  currency?: string;
  name?: string;
  trade_date?: string;
  trading_status?: string;
  trading_session_id?: string;
  trading_session_sub?: string;
  bid?: string;
  bid_size?: string;
  offer?: string;
  offer_size?: string;
  bids?: BookLevel[];
  offers?: BookLevel[];
  last?: string;
  last_size?: string;
  last_time?: string;
  open?: string;
  high?: string;
  low?: string;
  close?: string;
  wapr?: string;
  settle?: string;
  current_price?: string;
  official_price?: string;
  volume?: string;
  value?: string;
  updated_at?: string;
};

type Snapshot = {
  id: number;
  adapter_id: string;
  md_req_id: string;
  requested_at: string;
  completed_at?: string;
  status: string;
  report_count: number;
};

type Resp = {
  source?: string;
  note?: string;
  live?: boolean;
  snapshot?: Snapshot;
  quotes: Quote[];
};

function quoteKey(r: Quote): string {
  return `${r.board || ""}|${r.symbol || r.security_id || ""}`;
}

function mergeQuoteRow(prev: Quote | undefined, row: Quote): Quote {
  if (!prev) return row;
  const out: Quote = { ...prev };
  (Object.keys(row) as Array<keyof Quote>).forEach((k) => {
    const v = row[k];
    if (v === undefined || v === null || v === "") return;
    (out as Record<string, unknown>)[k as string] = v;
  });
  return out;
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

function tradingStatusLabel(code?: string, session?: string): string {
  const s = (session || "").toUpperCase();
  switch (code) {
    case "2":
      return "Приостановлены";
    case "17":
      return s && s !== "OPEN" ? s : "Идут";
    case "18":
      return "Закрыты";
    case "100":
      return "Закрытие";
    case "101":
      return "Открытие";
    case "102":
      return "Аукцион";
    case "103":
      return "Аукцион закрытия";
    case "104":
      return "Дискр. аукцион";
    default:
      return session || code || "—";
  }
}

function statusTone(code?: string): string {
  switch (code) {
    case "17":
      return "bg-emerald-50 text-emerald-700";
    case "2":
    case "18":
      return "bg-slate-100 text-slate-600";
    case "102":
    case "103":
    case "104":
    case "100":
    case "101":
      return "bg-amber-50 text-amber-800";
    default:
      return "bg-slate-50 text-slate-500";
  }
}

export default function MarketDataPage() {
  const qc = useQueryClient();
  const { connected, subscribe } = useAdminStream();
  const [q, setQ] = useState("");
  const [boardFilter, setBoardFilter] = useState("");
  const [symbolInput, setSymbolInput] = useState("");
  const [boardInput, setBoardInput] = useState("");

  const [flash, setFlash] = useState<Record<string, number>>({});
  const [selectedKey, setSelectedKey] = useState("");

  const dataQ = useQuery({
    queryKey: ["admin-market-data"],
    queryFn: ({ signal }) => api<Resp>("/api/v1/admin/market-data", { signal }),
    refetchInterval: liveRefetchInterval(connected, 2000, 15000),
  });

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type !== "marketdata.tick") return;
      const incoming = (msg.data as { quotes?: Quote[] } | undefined)?.quotes;
      if (!incoming?.length) return;
      qc.setQueryData<Resp>(["admin-market-data"], (old) => {
        const map = new Map<string, Quote>();
        for (const row of old?.quotes || []) map.set(quoteKey(row), row);
        const flashed: Record<string, number> = {};
        const now = Date.now();
        for (const row of incoming) {
          const key = quoteKey(row);
          const prev = map.get(key);
          const next = mergeQuoteRow(prev, row);
          if (
            !prev ||
            prev.last !== next.last ||
            prev.bid !== next.bid ||
            prev.offer !== next.offer ||
            prev.volume !== next.volume ||
            prev.value !== next.value ||
            (next.bids && next.bids.length !== (prev.bids?.length || 0)) ||
            (next.offers && next.offers.length !== (prev.offers?.length || 0))
          ) {
            flashed[key] = now;
          }
          map.set(key, next);
        }
        if (Object.keys(flashed).length) {
          setFlash((f) => ({ ...f, ...flashed }));
        }
        return {
          source: "live",
          live: true,
          snapshot: old?.snapshot,
          quotes: Array.from(map.values()),
        };
      });
    });
  }, [qc, subscribe]);

  useEffect(() => {
    if (!Object.keys(flash).length) return;
    const id = window.setTimeout(() => setFlash({}), 900);
    return () => window.clearTimeout(id);
  }, [flash]);

  const sync = useMutation({
    mutationFn: () =>
      api<{ summary?: string; received?: number; adapter_id?: string }>("/api/v1/admin/market-data/sync", {
        method: "POST",
        body: JSON.stringify({
          symbol: symbolInput.trim() || undefined,
          board: boardInput.trim() || undefined,
        }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-market-data"] });
    },
  });

  const quotes = dataQ.data?.quotes || [];
  const snapshot = dataQ.data?.snapshot;

  const boardOptions = useMemo(() => {
    const set = new Set<string>();
    quotes.forEach((r) => {
      if (r.board) set.add(r.board);
    });
    return Array.from(set).sort();
  }, [quotes]);

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const rows = quotes.filter((r) => {
      if (boardFilter && r.board !== boardFilter) return false;
      if (!needle) return true;
      return [r.symbol, r.security_id, r.board, r.name, r.currency, r.last, r.bid, r.offer]
        .join(" ")
        .toLowerCase()
        .includes(needle);
    });
    return rows.slice().sort((a, b) => {
      const board = (a.board || "").localeCompare(b.board || "", "ru");
      if (board) return board;
      const sa = a.symbol || a.security_id || "";
      const sb = b.symbol || b.security_id || "";
      const sym = sa.localeCompare(sb, "ru", { numeric: true, sensitivity: "base" });
      if (sym) return sym;
      return (a.security_id || "").localeCompare(b.security_id || "", "ru");
    });
  }, [quotes, q, boardFilter]);

  const selected = useMemo(
    () => filtered.find((r) => quoteKey(r) === selectedKey) || filtered[0] || null,
    [filtered, selectedKey],
  );

  return (
    <div className="space-y-4">
      <PageHeader
        title="Текущие торги"
        subtitle="ARQA FIX V→W/X · полный стакан (264=0) · котировки в реальном времени"
        actions={
          <div className="flex flex-wrap gap-2 text-xs">
            <div className="rounded-lg border border-line bg-panel px-3 py-1.5 text-ink">
              <span className={`mr-2 inline-block h-2 w-2 rounded-full ${connected ? "bg-emerald-500" : "bg-slate-300"}`} />
              <span className="text-slate-500">{connected ? "Онлайн" : "Нет сокета"}</span>
            </div>
            <Pill label="Инструментов" value={filtered.length} />
            {snapshot && <Pill label="Снимок" value={new Date(snapshot.requested_at).toLocaleString("ru-RU")} />}
          </div>
        }
      />

      <div className="card space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <input
            className="input min-w-[180px] flex-1"
            placeholder="Поиск: код, класс, цена…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <select
            className="input w-auto max-w-[180px]"
            value={boardFilter}
            onChange={(e) => setBoardFilter(e.target.value)}
          >
            <option value="">Класс: все</option>
            {boardOptions.map((b) => (
              <option key={b} value={b}>
                {b}
              </option>
            ))}
          </select>
          <input
            className="input w-36"
            placeholder="Код (48)"
            value={symbolInput}
            onChange={(e) => setSymbolInput(e.target.value)}
          />
          <input
            className="input w-36"
            placeholder="Класс (100)"
            value={boardInput}
            onChange={(e) => setBoardInput(e.target.value)}
          />
          <button className="btn-muted" onClick={() => void dataQ.refetch()}>
            Обновить
          </button>
          <button className="btn" disabled={sync.isPending} onClick={() => sync.mutate()}>
            {sync.isPending ? "Подписка…" : "Подписаться по FIX"}
          </button>
        </div>

        {dataQ.data?.note && (
          <div className="rounded-lg border border-amber-100 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            {dataQ.data.note}
          </div>
        )}
        {sync.isError && <div className="text-sm text-danger">{(sync.error as Error).message}</div>}
        {sync.isSuccess && (
          <div className="text-sm text-emerald-700">
            {sync.data?.summary || `Получено: ${sync.data?.received ?? 0}`}
            {sync.data?.adapter_id ? ` · адаптер ${sync.data.adapter_id}` : ""}
          </div>
        )}

        {dataQ.isLoading && <div className="py-8 text-center text-slate-400">Загрузка…</div>}

        {!dataQ.isLoading && (
          <div className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_280px]">
            <div className="overflow-x-auto rounded-lg border border-slate-200">
              <table className="w-full min-w-[1280px] text-left text-sm">
                <thead className="sticky top-0 z-10 bg-slate-50">
                  <tr>
                    {[
                      "Код",
                      "Класс",
                      "Посл.",
                      "Спрос",
                      "Предл.",
                      "Стакан",
                      "Откр.",
                      "Макс.",
                      "Мин.",
                      "Закр.",
                      "Средневзв.",
                      "Объём",
                      "Оборот",
                      "Статус",
                    ].map((h) => (
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
                  {filtered.length === 0 && (
                    <tr>
                      <td colSpan={14} className="px-3 py-10 text-center text-slate-400">
                        Нет котировок — запросите по FIX
                      </td>
                    </tr>
                  )}
                  {filtered.map((r) => {
                    const last = r.last || r.current_price || r.official_price;
                    const key = quoteKey(r);
                    const hot = !!flash[key];
                    const active = selected && quoteKey(selected) === key;
                    const bidN = r.bids?.length || (r.bid ? 1 : 0);
                    const offerN = r.offers?.length || (r.offer ? 1 : 0);
                    return (
                      <tr
                        key={key}
                        onClick={() => setSelectedKey(key)}
                        className={`cursor-pointer border-b border-slate-100 transition-colors ${
                          active ? "bg-sky-50" : hot ? "bg-amber-50" : "hover:bg-slate-50"
                        }`}
                      >
                        <td className="px-3 py-2">
                          <div className="font-mono text-xs">{r.symbol || r.security_id || "—"}</div>
                          {r.name ? <div className="text-[11px] text-slate-400">{r.name}</div> : null}
                        </td>
                        <td className="px-3 py-2 font-mono text-xs text-slate-600">{r.board || "—"}</td>
                        <td className="px-3 py-2">
                          <div className="font-mono text-sm font-semibold tabular-nums">{fmt(last)}</div>
                          {r.last_time ? (
                            <div className="font-mono text-[11px] text-slate-400">{r.last_time}</div>
                          ) : null}
                        </td>
                        <td className="px-3 py-2 font-mono tabular-nums text-emerald-700">
                          {fmt(r.bid)}
                          {r.bid_size ? <span className="ml-1 text-[11px] text-slate-400">{fmt(r.bid_size, 0)}</span> : null}
                        </td>
                        <td className="px-3 py-2 font-mono tabular-nums text-rose-700">
                          {fmt(r.offer)}
                          {r.offer_size ? (
                            <span className="ml-1 text-[11px] text-slate-400">{fmt(r.offer_size, 0)}</span>
                          ) : null}
                        </td>
                        <td className="px-3 py-2 font-mono text-[11px] tabular-nums text-slate-500">
                          {bidN}×{offerN}
                        </td>
                        <td className="px-3 py-2 font-mono tabular-nums">{fmt(r.open)}</td>
                        <td className="px-3 py-2 font-mono tabular-nums">{fmt(r.high)}</td>
                        <td className="px-3 py-2 font-mono tabular-nums">{fmt(r.low)}</td>
                        <td className="px-3 py-2 font-mono tabular-nums">{fmt(r.close)}</td>
                        <td className="px-3 py-2 font-mono tabular-nums">{fmt(r.wapr)}</td>
                        <td className="px-3 py-2 font-mono tabular-nums">{fmt(r.volume, 0)}</td>
                        <td className="px-3 py-2 font-mono tabular-nums">{fmt(r.value, 0)}</td>
                        <td className="px-3 py-2">
                          <span className={`inline-flex rounded-md px-2 py-0.5 text-[11px] font-semibold ${statusTone(r.trading_status)}`}>
                            {tradingStatusLabel(r.trading_status, r.trading_session_id)}
                          </span>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <OrderBookPanel quote={selected} />
          </div>
        )}

        {snapshot && (
          <div className="text-xs text-slate-400">
            Источник: FIX Market Data · адаптер {snapshot.adapter_id} · MDReqID {snapshot.md_req_id} · строк{" "}
            {snapshot.report_count}
          </div>
        )}
      </div>
    </div>
  );
}

function bookSides(q: Quote | null): { bids: BookLevel[]; offers: BookLevel[] } {
  if (!q) return { bids: [], offers: [] };
  const bids = q.bids && q.bids.length ? q.bids : q.bid ? [{ price: q.bid, size: q.bid_size }] : [];
  const offers = q.offers && q.offers.length ? q.offers : q.offer ? [{ price: q.offer, size: q.offer_size }] : [];
  return { bids, offers };
}

function OrderBookPanel({ quote }: { quote: Quote | null }) {
  const { bids, offers } = bookSides(quote);
  return (
    <div className="rounded-lg border border-slate-200 bg-slate-50/80 p-3">
      <div className="mb-2 text-[12px] font-medium text-muted">Стакан</div>
      {quote ? (
        <div className="mb-2 font-mono text-xs text-slate-600">
          {quote.symbol || quote.security_id} · {quote.board || "—"}
        </div>
      ) : (
        <div className="text-xs text-slate-400">Выберите инструмент</div>
      )}
      <div className="grid grid-cols-2 gap-2 text-xs">
        <div>
          <div className="mb-1 font-medium text-emerald-700">Спрос</div>
          {bids.length === 0 && <div className="text-slate-400">нет заявок</div>}
          {bids.map((l, i) => (
            <div key={`b-${l.price}-${i}`} className="flex justify-between font-mono tabular-nums">
              <span className="text-emerald-700">{fmt(l.price)}</span>
              <span className="text-slate-500">{fmt(l.size, 0)}</span>
            </div>
          ))}
        </div>
        <div>
          <div className="mb-1 font-medium text-rose-700">Предл.</div>
          {offers.length === 0 && <div className="text-slate-400">нет заявок</div>}
          {offers.map((l, i) => (
            <div key={`o-${l.price}-${i}`} className="flex justify-between font-mono tabular-nums">
              <span className="text-rose-700">{fmt(l.price)}</span>
              <span className="text-slate-500">{fmt(l.size, 0)}</span>
            </div>
          ))}
        </div>
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
