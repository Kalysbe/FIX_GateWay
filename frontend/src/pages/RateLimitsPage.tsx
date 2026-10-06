import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, getSessionUser } from "../api";
import { PageHeader } from "../components/PageHeader";
import { TradingLimits } from "../components/TradingLimits";

type Policy = { scope: string; subject: string; operation: string; rate: number; burst: number; concurrent: number; enabled: boolean };
type Usage = Policy & { current_rps: number; peak_rps: number; active: number; rejected: number };
type Result = { policies: Policy[]; usage: Usage[]; operations: string[] };
const key = (p: Policy) => `${p.scope}:${p.subject}:${p.operation}`;
const empty: Policy = { scope: "broker", subject: "", operation: "SubmitOrder", rate: 100, burst: 100, concurrent: 16, enabled: true };

export default function RateLimitsPage() {
  const q = useQuery({ queryKey: ["rate-limits"], queryFn: ({ signal }) => api<Result>("/api/v1/admin/rate-limits", { signal }), refetchInterval: 3000 });
  const [draft, setDraft] = useState<Policy>(empty);
  const [reason, setReason] = useState("");
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const user = getSessionUser();
  const canEdit = user?.role === "ADMIN" || user?.role === "SUPER_ADMIN";
  const rows = new Map<string, Policy | Usage>();
  q.data?.policies.forEach(p => rows.set(key(p), p));
  q.data?.usage.forEach(p => rows.set(key(p), p));
  async function save() {
    setSaving(true); setMessage("");
    try { await api("/api/v1/admin/rate-limits", { method: "PUT", body: JSON.stringify({ ...draft, reason }) }); setMessage("Лимит сохранён"); await q.refetch(); }
    catch (error) { setMessage(error instanceof Error ? error.message : "Ошибка сохранения"); }
    finally { setSaving(false); }
  }
  return <>
    <PageHeader title="Rate Limits" subtitle="Лимиты брокеров, операций и глобальная защита" />
    <TradingLimits />
    <p className="mb-4 text-sm text-muted">Статистика текущего Gateway. Current — запросы в текущей секунде. Категории и операции применяются совместно. Изменения вступают в силу без перезапуска.</p>
    {q.error && <p role="alert">{q.error.message}</p>}
    <div className="overflow-x-auto rounded-lg border border-line bg-panel">
      <table className="w-full text-sm"><thead><tr>{["Scope / Broker", "Operation", "Rate / s", "Burst", "Concurrent", "Current / s", "Peak / s", "Rejected", "Status", ""].map(x => <th key={x} className="p-3 text-left">{x}</th>)}</tr></thead>
        <tbody>{[...rows.values()].map(p => { const usage = p as Partial<Usage>; return <tr key={key(p)} className="border-t border-line">
          <td className="p-3">{p.scope} / {p.subject}</td><td>{p.operation}</td><td>{p.rate}</td><td>{p.burst}</td><td>{usage.active ?? 0} / {p.concurrent}</td><td>{usage.current_rps ?? 0}</td><td>{usage.peak_rps ?? 0}</td><td>{usage.rejected ?? 0}</td><td>{p.enabled ? "ACTIVE" : "DISABLED"}</td>
          <td>{canEdit && !(p.scope === "broker" && p.subject === "*") && <button className="text-accent" onClick={() => { setDraft({ scope: p.scope, subject: p.scope === "admin" ? "*" : p.subject, operation: p.operation, rate: p.rate, burst: p.burst, concurrent: p.concurrent, enabled: p.enabled }); setMessage(""); }}>Изменить</button>}</td>
        </tr>; })}</tbody>
      </table>
    </div>
    {canEdit && <form className="mt-6 grid gap-3 rounded-lg border border-line bg-panel p-4 md:grid-cols-3" onSubmit={e => { e.preventDefault(); void save(); }}>
      <label>Scope<select className="input w-full" value={draft.scope} onChange={e => setDraft({ ...draft, scope: e.target.value, subject: e.target.value === "broker" ? "" : "*", operation: e.target.value === "admin" ? "System" : e.target.value === "global" ? "Trading" : "SubmitOrder" })}>{["broker", "global", "admin"].map(v => <option key={v}>{v}</option>)}</select></label>
      <label>Broker ID<input className="input w-full" required value={draft.subject} disabled={draft.scope !== "broker"} onChange={e => setDraft({ ...draft, subject: e.target.value })} /></label>
      <label>Operation<select className="input w-full" value={draft.operation} onChange={e => setDraft({ ...draft, operation: e.target.value })}>{q.data?.operations.filter(v => draft.scope === "global" ? ["*", "Trading", "Queries", "System"].includes(v) : draft.scope === "admin" ? ["*", "System", "Connect", "Subscribe", "Ping"].includes(v) : !["Connect", "Subscribe", "Ping"].includes(v)).map(v => <option key={v}>{v}</option>)}</select></label>
      {(["rate", "burst", "concurrent"] as const).map(field => <label key={field}>{field}<input className="input w-full" type="number" min={1} max={1000000} required value={draft[field]} onChange={e => setDraft({ ...draft, [field]: Number(e.target.value) })} /></label>)}
      <label className="flex items-center gap-2"><input type="checkbox" checked={draft.enabled} onChange={e => setDraft({ ...draft, enabled: e.target.checked })} />Ограничение включено</label>
      <label>Причина изменения<input className="input w-full" minLength={3} maxLength={1000} required value={reason} onChange={e => setReason(e.target.value)} /></label>
      <button className="btn-primary" disabled={saving}>{saving ? "Сохранение…" : "Сохранить"}</button>
      {message && <p role="status" className="md:col-span-3">{message}</p>}
    </form>}
  </>;
}
