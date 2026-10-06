import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, getSessionUser } from "../api";
import { formatDateTime } from "../labels";
import { ErrorBanner } from "./feedback";

type Case = {
  event_id: number; order_id: string; client_order_id: string; event_type: string;
  order_status: string; last_error: string; evidence_event_id: number | null;
  resolved_at: string | null; actor_id: string | null; note: string | null;
};

export function ReconciliationQueue({ orderId }: { orderId?: string }) {
  const [notes, setNotes] = useState<Record<number, string>>({});
  const role = getSessionUser()?.role;
  const canResolve = ["SUPER_ADMIN", "ADMIN", "OPERATOR"].includes(role || "");
  const q = useQuery({
    queryKey: ["reconciliation", orderId],
    queryFn: ({ signal }) => api<{ cases: Case[] }>(`/api/v1/admin/reconciliation?limit=100${orderId ? `&order_id=${encodeURIComponent(orderId)}` : ""}`, { signal }),
    refetchInterval: 5000,
  });
  const resolve = useMutation({
    mutationFn: ({ id, note }: { id: number; note: string }) => api(`/api/v1/admin/reconciliation/${id}/resolve`, {
      method: "POST", body: JSON.stringify({ note }),
    }),
    onSuccess: () => { void q.refetch(); },
  });
  const cases = q.data?.cases || [];
  return <section className="card space-y-3">
    <h2 className="font-medium">Сверка неопределённых отправок</h2>
    <p className="text-sm text-slate-500">Шлюз автоматически закрывает случаи по сохранённому FIX-подтверждению. Для отмены требуется конечный статус заявки. Оператор также может закрыть подтверждённый случай с комментарием. История решения сохраняется.</p>
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    {resolve.error && <ErrorBanner error={resolve.error} onRetry={() => resolve.reset()} />}
    {q.isLoading && <p className="text-sm">Загрузка…</p>}
    {!q.isLoading && !q.error && cases.length === 0 && <p className="text-sm text-slate-500">Случаев для сверки нет.</p>}
    {cases.map(c => <div key={c.event_id} className="rounded-lg border border-slate-200 p-3 space-y-2 text-sm">
      <div><Link className="text-blue-600" to={`/orders/${c.order_id}`}>{c.client_order_id}</Link> · {c.event_type === "ORDER_CANCEL" ? "Отмена" : "Отправка"} · {c.order_status}</div>
      <p className="break-words text-slate-500">{c.last_error}</p>
      {c.resolved_at ? <p>Закрыто {formatDateTime(c.resolved_at)} · {c.actor_id === "system:fix-reconciliation" ? "Автоматическая сверка FIX" : c.actor_id} · {c.actor_id === "system:fix-reconciliation" ? "По сохранённому подтверждению торгового контура" : c.note}</p> : <>
        <p>{c.evidence_event_id ? `Есть FIX-подтверждение: событие №${c.evidence_event_id}` : "Подтверждение отсутствует. Требуется сверка с торговым контуром."}</p>
        {canResolve && c.evidence_event_id && <div className="flex flex-wrap gap-2">
          <input className="input flex-1" aria-label={`Комментарий к случаю ${c.event_id}`} placeholder="Комментарий оператора" maxLength={2000} value={notes[c.event_id] || ""} onChange={e => setNotes({ ...notes, [c.event_id]: e.target.value })} />
          <button className="btn" disabled={resolve.isPending || !(notes[c.event_id] || "").trim()} onClick={() => resolve.mutate({ id: c.event_id, note: notes[c.event_id] })}>Закрыть по подтверждению</button>
        </div>}
      </>}
    </div>)}
    {cases.length === 100 && <p className="text-sm text-slate-500">Показаны первые 100 случаев. Историю конкретной заявки можно открыть в её карточке.</p>}
  </section>;
}
