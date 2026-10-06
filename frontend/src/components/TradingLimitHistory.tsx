import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, getSessionUser } from "../api";
import { formatDateTime } from "../labels";
import { ErrorBanner } from "./feedback";

type Policy = { paused: boolean; max_quantity: string | null; max_open_orders: number | null; revision: number };
type Change = { id: number; broker_id: number; actor_id: string; reason: string; before: Policy | null; after: Policy; created_at: string };
export function TradingLimitHistory() {
  const [broker, setBroker] = useState("");
  const [cursor, setCursor] = useState<number | null>(null);
  const allowed = ["SUPER_ADMIN", "ADMIN", "OPERATOR", "AUDITOR"].includes(getSessionUser()?.role || "");
  const q = useQuery({
    queryKey: ["trading-limit-history", broker, cursor],
    enabled: allowed,
    queryFn: ({ signal }) => api<{ history: Change[]; next_before_id: number | null }>(`/api/v1/admin/trading-limits/history?limit=20${broker ? `&broker_id=${encodeURIComponent(broker)}` : ""}${cursor ? `&before_id=${cursor}` : ""}`, { signal }),
    refetchInterval: cursor ? false : 10000,
  });
  if (!allowed) return null;
  return <section className="space-y-3">
    <h3 className="font-medium">История торговых ограничений</h3>
    <label className="block text-sm">ID брокера (0 — общая политика)
      <input className="input ml-2" inputMode="numeric" placeholder="Все политики" value={broker} onChange={e => { if (/^\d*$/.test(e.target.value)) { setBroker(e.target.value); setCursor(null); } }} />
    </label>
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    {q.isLoading && <p>Загрузка истории…</p>}
    {!q.isLoading && !q.error && q.data?.history.length === 0 && <p className="text-sm text-muted">Изменений пока нет.</p>}
    {q.data?.history.map(c => <article className="rounded-lg border border-line p-3 text-sm space-y-1" key={c.id}>
      <p>{formatDateTime(c.created_at)} · {c.broker_id === 0 ? "Общая политика" : `Брокер №${c.broker_id}`} · {c.actor_id}</p>
      <p className="break-words">Причина: {c.reason}</p>
      {!c.before && <p>Создана новая политика</p>}
      <p>Приём: {c.before ? (c.before.paused ? "пауза" : "разрешён") : "—"} → {c.after.paused ? "пауза" : "разрешён"}</p>
      <p>Количество: {c.before ? c.before.max_quantity ?? "без ограничения" : "—"} → {c.after.max_quantity ?? "без ограничения"}</p>
      <p>Открытые заявки: {c.before ? c.before.max_open_orders ?? "без ограничения" : "—"} → {c.after.max_open_orders ?? "без ограничения"}</p>
      <p className="text-muted">Версия: {c.before?.revision ?? "—"} → {c.after.revision}</p>
    </article>)}
    <div className="flex gap-2">
      {cursor && <button className="btn" onClick={() => setCursor(null)}>Последние изменения</button>}
      {q.data?.next_before_id && <button className="btn" onClick={() => setCursor(q.data!.next_before_id)}>Более ранние изменения</button>}
    </div>
  </section>;
}
