import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, getSessionUser } from "../api";
import { ErrorBanner } from "./feedback";
import { TradingLimitHistory } from "./TradingLimitHistory";
import { useQueryClient } from "@tanstack/react-query";

type Policy = { broker_id: number; paused: boolean; max_quantity: string | null; max_open_orders: number | null; revision: number };
export function TradingLimits() {
  const queryClient = useQueryClient();
  const q = useQuery({ queryKey: ["trading-limits"], queryFn: ({ signal }) => api<{ policies: Policy[] }>("/api/v1/admin/trading-limits", { signal }), refetchInterval: 5000 });
  const [broker, setBroker] = useState("0");
  const [draft, setDraft] = useState<Policy | null>(null);
  const [quantity, setQuantity] = useState("");
  const [open, setOpen] = useState("");
  const [reason, setReason] = useState("");
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const role = getSessionUser()?.role;
  const canEdit = role === "ADMIN" || role === "SUPER_ADMIN";
  function edit(p: Policy) {
    setBroker(String(p.broker_id)); setDraft(p); setQuantity(p.max_quantity || "");
    setOpen(p.max_open_orders == null ? "" : String(p.max_open_orders)); setMessage(""); setReason("");
  }
  async function save() {
    if (!draft) return;
    setSaving(true); setMessage("");
    try {
      await api("/api/v1/admin/trading-limits", { method: "PUT", body: JSON.stringify({ broker_id: draft.broker_id, paused: draft.paused, revision: draft.revision, max_quantity: quantity || null, max_open_orders: open ? Number(open) : null, reason }) });
      setMessage("Торговый лимит сохранён"); setDraft(null); await q.refetch();
      await queryClient.invalidateQueries({ queryKey: ["trading-limit-history"] });
    } catch (e) { setMessage(e instanceof Error ? e.message : "Ошибка сохранения"); }
    finally { setSaving(false); }
  }
  return <section className="card my-6 space-y-3">
    <h2 className="font-medium">Торговые ограничения</h2>
    <p className="text-sm text-muted">Общий и персональный лимиты действуют совместно. Пауза блокирует приём новых заявок. Уже принятые заявки продолжают обрабатываться; отмены доступны. Общий лимит открытых заявок применяется к каждому брокеру отдельно.</p>
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    {q.isLoading && <p>Загрузка…</p>}
    {q.data?.policies.map(p => <div key={p.broker_id} className="flex flex-wrap items-center gap-3 border-b border-line py-2 text-sm">
      <strong>{p.broker_id === 0 ? "Все брокеры" : `Брокер №${p.broker_id}`}</strong>
      <span>{p.paused ? "Приём приостановлен" : "Приём разрешён"}</span>
      <span>Количество: {p.max_quantity ?? "без ограничения"}</span>
      <span>Открытых заявок: {p.max_open_orders ?? "без ограничения"}</span>
      {canEdit && <button className="btn" onClick={() => edit(p)}>Изменить</button>}
    </div>)}
    {canEdit && <div className="flex flex-wrap gap-2">
      <input className="input" type="number" min={1} aria-label="ID брокера для персонального лимита" value={broker} onChange={e => setBroker(e.target.value)} />
      <button className="btn" disabled={!/^\d+$/.test(broker) || !Number.isSafeInteger(Number(broker)) || saving || !q.data} onClick={() => edit(q.data?.policies.find(p => p.broker_id === Number(broker)) || { broker_id: Number(broker), paused: false, max_quantity: null, max_open_orders: null, revision: 0 })}>Настроить брокера</button>
    </div>}
    {canEdit && draft && <form className="grid gap-3 md:grid-cols-2" onSubmit={e => { e.preventDefault(); void save(); }}>
      <p className="md:col-span-2">{draft.broker_id === 0 ? "Общая политика" : `Политика брокера №${draft.broker_id}`}</p>
      <label>Максимальное количество<input className="input w-full" inputMode="decimal" pattern="[0-9]+([.][0-9]+)?" value={quantity} onChange={e => setQuantity(e.target.value)} placeholder="Без ограничения" /></label>
      <label>Максимум открытых заявок<input className="input w-full" type="number" min={1} max={1000000} value={open} onChange={e => setOpen(e.target.value)} placeholder="Без ограничения" /></label>
      <label className="flex items-center gap-2"><input type="checkbox" checked={draft.paused} onChange={e => setDraft({ ...draft, paused: e.target.checked })} />Приостановить приём новых заявок</label>
      <label>Причина изменения<input className="input w-full" required minLength={3} maxLength={1000} value={reason} onChange={e => setReason(e.target.value)} /></label>
      <button className="btn" disabled={saving}>{saving ? "Сохранение…" : "Сохранить"}</button>
    </form>}
    {message && <p role="status">{message}</p>}
    <TradingLimitHistory />
  </section>;
}
