import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { DataTable, type TableColumn } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";
import { numberValue } from "../lib/table";
import { hasReceivedMarketData, instrumentStatus, marketDataSummary } from "../lib/marketData";

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
  raw_fix?: string;
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
  subscription?: Subscription;
};

type Subscription = { adapter_id?: string; status: string; expected: number; confirmed: number; error?: string; next_retry_at?: string; last_received_at?: string };
const subscriptionLabels: Record<string, string> = { ACTIVE: "Подписка активна", STARTING: "Подключение потока", WAITING: "Ожидание инструментов", RETRYING: "Восстановление потока", PARTIAL: "Частичный поток", OFFLINE: "Ожидание FIX-связи" };

function quoteKey(r: Quote): string {
  return `${r.board || ""}|${r.symbol || r.security_id || ""}`;
}

function mergeQuoteRow(prev: Quote | undefined, row: Quote): Quote {
  if (!prev) return row;
  const out: Quote = { ...prev };
  (Object.keys(row) as Array<keyof Quote>).forEach((k) => {
    const v = row[k];
    if (v === undefined || v === null) return;
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

export default function MarketDataPage() {
  const qc = useQueryClient();
  const { connected, subscribe } = useAdminStream();
  const [boardFilter, setBoardFilter] = useState("");

  const [flash, setFlash] = useState<Record<string, number>>({});
  const [selectedKey, setSelectedKey] = useState("");

  const dataQ = useQuery({
    queryKey: ["admin-market-data"],
    queryFn: ({ signal }) => api<Resp>("/api/v1/admin/market-data", { signal }),
    refetchInterval: liveRefetchInterval(connected, 2000, 5000),
  });

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type !== "marketdata.tick") return;
      const payload = msg.data as { quotes?: Quote[]; subscription?: Subscription; replace?: boolean } | undefined;
      const incoming = payload?.quotes || [];
      if (!incoming.length && !payload?.subscription && !payload?.replace) return;
      qc.setQueryData<Resp>(["admin-market-data"], (old) => {
        const map = new Map<string, Quote>();
        const changedAdapter = payload?.subscription?.adapter_id && payload.subscription.adapter_id !== old?.subscription?.adapter_id;
        if (!payload?.replace && !changedAdapter) for (const row of old?.quotes || []) map.set(quoteKey(row), row);
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
          live: payload?.subscription ? ["ACTIVE", "PARTIAL"].includes(payload.subscription.status) : old?.live,
          subscription: payload?.subscription || old?.subscription,
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

  const quotes = dataQ.data?.quotes || [];
  const snapshot = dataQ.data?.snapshot;
  const subscription = dataQ.data?.subscription;

  const boardOptions = useMemo(() => {
    const set = new Set<string>();
    quotes.forEach((r) => {
      if (r.board) set.add(r.board);
    });
    return Array.from(set).sort();
  }, [quotes]);

  const filtered = useMemo(() => {
    const rows = quotes.filter((r) => {
      if (boardFilter && r.board !== boardFilter) return false;
      return true;
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
  }, [quotes, boardFilter]);

  const selected = useMemo(
    () => filtered.find((r) => quoteKey(r) === selectedKey) || filtered[0] || null,
    [filtered, selectedKey],
  );
  const missingData = filtered.filter(r => !hasReceivedMarketData(r)).length;
  const missingStatus = filtered.filter(r => !instrumentStatus(r.trading_status, r.trading_session_id).reported).length;

  const columns: TableColumn<Quote>[] = [
    { id: "symbol", header: "Инструмент", value: r => r.symbol || r.security_id, cell: r => <button type="button" className="text-left" onClick={() => setSelectedKey(quoteKey(r))} aria-label={"Открыть стакан " + (r.symbol || r.security_id)} aria-pressed={selected != null && quoteKey(selected) === quoteKey(r)}><strong className="font-mono text-accent">{r.symbol || r.security_id}</strong><div className="text-[10px] text-muted">{r.name}</div></button> },
    { id: "board", header: "Класс", value: r => r.board, className: "font-mono text-[10px]" },
    { id: "last", header: "Последняя", value: r => numberValue(r.last || r.current_price || r.official_price), cell: r => <strong>{fmt(r.last || r.current_price || r.official_price)}</strong>, numeric: true },
    { id: "bid", header: "Спрос", value: r => numberValue(r.bid), cell: r => <span className="text-emerald-700">{fmt(r.bid)}<small className="ml-1 text-[9px] text-muted">{r.bid_size ? "× " + fmt(r.bid_size, 0) : ""}</small></span>, numeric: true },
    { id: "offer", header: "Предложение", value: r => numberValue(r.offer), cell: r => <span className="text-rose-700">{fmt(r.offer)}<small className="ml-1 text-[9px] text-muted">{r.offer_size ? "× " + fmt(r.offer_size, 0) : ""}</small></span>, numeric: true },
    { id: "depth", header: "Стакан", value: r => (r.bids?.length || (r.bid ? 1 : 0)) + "×" + (r.offers?.length || (r.offer ? 1 : 0)) },
    ...([ ["open", "Открытие"], ["high", "Максимум"], ["low", "Минимум"], ["close", "Закрытие"], ["wapr", "Средневзв."], ["volume", "Объём"], ["value", "Оборот"] ] as const).map(([key, header]) => ({ id: key, header, value: (r: Quote) => numberValue(r[key]), cell: (r: Quote) => fmt(r[key], key === "volume" ? 0 : 2), numeric: true })),
    { id: "status", header: "Статус", value: r => instrumentStatus(r.trading_status, r.trading_session_id).label, cell: r => {
      const status = instrumentStatus(r.trading_status, r.trading_session_id);
      return <span className={`status-badge ${status.tone}`} title={status.reported ? [r.trading_status && `FIX 326: ${r.trading_status}`, r.trading_session_id, r.trading_session_sub].filter(Boolean).join(" · ") : "FIX-адаптер пока не передал торговый статус инструмента"}><i aria-hidden="true" />{status.label}</span>;
    } },
  ];

  return (
    <div className="space-y-4">
      <PageHeader
        title="Текущие торги"
        subtitle="Рынок в реальном времени · выберите инструмент, чтобы открыть стакан"
        actions={
          <div className="flex flex-wrap gap-2 text-xs">
            <div className={`status-badge ${subscription?.status === "ACTIVE" ? "badge-success" : "badge-warn"}`} title="Шлюз автоматически поддерживает подписку независимо от открытой страницы"><i aria-hidden="true" />{subscriptionLabels[subscription?.status || "STARTING"] || "Автоматическая подписка"}{subscription && ` · ${subscription.confirmed}/${subscription.expected}`}</div>
            <div className="rounded-lg border border-line bg-panel px-3 py-1.5 text-ink" title="Соединение с потоком событий консоли. Получение рыночных данных учитывается отдельно.">
              <span className={`mr-2 inline-block h-2 w-2 rounded-full ${connected ? "bg-emerald-500" : "bg-slate-300"}`} />
              <span className="text-slate-500">{connected ? "Консоль подключена" : "Консоль: опрос"}</span>
            </div>
            <Pill label="Инструментов" value={filtered.length} />
            {subscription?.last_received_at ? <Pill label="Данные" value={new Date(subscription.last_received_at).toLocaleTimeString("ru-RU")} /> : snapshot && <Pill label={dataQ.data?.source === "live" ? "Проверка" : "Снимок"} value={new Date(snapshot.requested_at).toLocaleString("ru-RU")} />}
          </div>
        }
      />

      <div className="card space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <select
            className="input w-auto max-w-[180px]" aria-label="Класс инструментов"
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
          <span className="text-xs text-muted">Подписка и восстановление выполняются автоматически</span>
        </div>

        {dataQ.data?.note && (
          <div className="rounded-lg border border-amber-100 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            {dataQ.data.note}
          </div>
        )}
        {dataQ.error && <ErrorBanner error={dataQ.error} />}
        {subscription?.error && <div role="status" className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900"><strong>Автоматическое восстановление потока.</strong> {subscription.error}{subscription.next_retry_at && ` · следующая проверка ${new Date(subscription.next_retry_at).toLocaleTimeString("ru-RU")}`}</div>}
        {!dataQ.isLoading && !dataQ.error && missingStatus > 0 && (
          <div role="status" className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900">
            {missingData > 0 ? <><strong>Рыночные данные не получены по {missingData} из {filtered.length} инструментов.</strong> Шлюз поддерживает подписку и повторяет подключение автоматически. </> : null}
            Статус не передан по {missingStatus} инструментам. Пока он неизвестен, подтвердить режим торгов нельзя.
          </div>
        )}
        <div className="grid items-start gap-3 2xl:grid-cols-[minmax(0,1fr)_240px]">
          <DataTable title="Текущие торги" rows={filtered} columns={columns} rowKey={quoteKey} loading={dataQ.isLoading} resetKey={boardFilter} exportName="market-data" searchPlaceholder="Инструмент, класс, название…" getSearchText={r => [r.symbol, r.security_id, r.board, r.name, r.currency, r.last, r.bid, r.offer, instrumentStatus(r.trading_status, r.trading_session_id).label].join(" ")} rowClassName={r => selected && quoteKey(selected) === quoteKey(r) ? "market-selected" : flash[quoteKey(r)] ? "market-tick" : ""} emptyTitle="Ожидание рыночных данных" emptyHint="Шлюз автоматически подключает инструменты из справочника" summary={marketDataSummary} />
          <OrderBookPanel quote={selected} />
        </div>

        {snapshot && (
          <div className="text-xs text-muted">
            Источник: FIX Market Data · адаптер {snapshot.adapter_id}{snapshot.md_req_id ? ` · MDReqID ${snapshot.md_req_id}` : ""} · данные получены по {filtered.length - missingData} из {filtered.length} инструментов
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
        <div className="text-xs text-muted">Выберите инструмент</div>
      )}
      <div className="grid grid-cols-2 gap-2 text-xs">
        <div>
          <div className="mb-1 font-medium text-emerald-700">Спрос</div>
          {bids.length === 0 && <div className="text-muted">нет заявок</div>}
          {bids.map((l, i) => (
            <div key={`b-${l.price}-${i}`} className="flex justify-between font-mono tabular-nums">
              <span className="text-emerald-700">{fmt(l.price)}</span>
              <span className="text-slate-500">{fmt(l.size, 0)}</span>
            </div>
          ))}
        </div>
        <div>
          <div className="mb-1 font-medium text-rose-700">Предл.</div>
          {offers.length === 0 && <div className="text-muted">нет заявок</div>}
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
