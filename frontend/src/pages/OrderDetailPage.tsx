import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, formatTime, sideLabel, statusLabel } from "../labels";

export default function OrderDetailPage() {
  const { id } = useParams();
  const q = useQuery({
    queryKey: ["order", id],
    queryFn: () => api<{ order: any; events: any[]; trades: any[] }>(`/api/v1/admin/orders/${id}`),
    refetchInterval: 2000,
  });
  const trace = useQuery({
    queryKey: ["order-trace", id],
    queryFn: () => api<{ events: any[] }>(`/api/v1/admin/orders/${id}/trace`),
    refetchInterval: 3000,
  });

  if (q.isLoading) return <div>Загрузка…</div>;
  const o = q.data?.order;
  const events = q.data?.events || [];
  const trades = q.data?.trades || [];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Ордер {o?.client_order_id}</h1>
        <p className="font-mono text-sm text-slate-500">{o?.id}</p>
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="card space-y-2 text-sm">
          <Row k="Статус" v={<span className={`status-${o?.status}`}>{statusLabel(o?.status)}</span>} />
          <Row k="Фирма" v={o?.firm_name || o?.quik_firm || "—"} />
          <Row k="Код фирмы" v={o?.quik_firm || "—"} />
          <Row k="Код клиента" v={o?.client_code || "—"} />
          <Row k="ID заявки клиента" v={o?.client_order_id || "—"} />
          <Row k="Инструмент" v={o?.symbol} />
          <Row k="Сторона" v={sideLabel(o?.side)} />
          <Row k="Количество" v={o?.quantity} />
          <Row k="Цена" v={o?.price ?? "—"} />
          <Row k="Исполнено" v={o?.filled_quantity} />
          <Row k="Адаптер" v={o?.adapter_id || "—"} />
          <Row k="FIX ClOrdID" v={o?.fix_cl_ord_id || "—"} />
          <Row k="QUIK OrderID" v={o?.quik_order_id || "—"} />
          <Row k="Request ID" v={o?.request_id || "—"} />
          <Row k="Trace ID" v={o?.trace_id || "—"} />
        </div>
        <div className="card">
          <h2 className="mb-3 font-medium">Жизненный цикл</h2>
          <ol className="space-y-2">
            {events.map((e) => {
              const raw = e.raw_fix || e.RawFIX || "";
              return (
                <li key={e.ID || e.id} className="rounded-lg bg-surface px-3 py-2 text-sm">
                  <div className="font-mono text-xs text-slate-500">
                    {formatTime(e.CreatedAt || e.created_at)}
                  </div>
                  <div className="font-medium">{e.EventType || e.event_type}</div>
                  <div className="text-slate-600">
                    {statusLabel(e.FromStatus || e.from_status)} → {statusLabel(e.ToStatus || e.to_status)}
                    {e.AdapterID || e.adapter_id ? ` · ${e.AdapterID || e.adapter_id}` : ""}
                  </div>
                  <div className="text-slate-500">{e.Message || e.message}</div>
                  {raw ? (
                    <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded border border-slate-200 bg-slate-50 p-2 font-mono text-[11px] leading-relaxed text-emerald-700">
                      {raw}
                    </pre>
                  ) : null}
                </li>
              );
            })}
          </ol>
        </div>
      </div>

      <div className="card overflow-x-auto">
        <h2 className="mb-3 font-medium">Сделки (FIX)</h2>
        {trades.length === 0 ? (
          <p className="text-sm text-slate-500">Нет исполнений по этой заявке</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Время</th>
                <th>ExecID</th>
                <th>Кол-во</th>
                <th>Цена</th>
                <th>Накоплено</th>
                <th>Остаток</th>
                <th>Адаптер</th>
              </tr>
            </thead>
            <tbody>
              {trades.map((t) => (
                <tr key={t.id}>
                  <td className="text-xs">{formatDateTime(t.created_at)}</td>
                  <td className="font-mono text-xs">{t.exec_id}</td>
                  <td className="font-mono">{t.last_qty}</td>
                  <td className="font-mono">{t.last_px}</td>
                  <td className="font-mono text-xs">{t.cum_qty}</td>
                  <td className="font-mono text-xs">{t.leaves_qty}</td>
                  <td className="text-xs">{t.adapter_id || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="card">
        <h2 className="mb-3 font-medium">Полный trace (Gateway → FIX)</h2>
        {(trace.data?.events || []).length === 0 ? (
          <p className="text-sm text-slate-500">События observability появятся по мере прохождения заявки</p>
        ) : (
          <ol className="space-y-2">
            {(trace.data?.events || []).slice().reverse().map((e: any) => (
              <li key={e.id} className="rounded-lg bg-surface px-3 py-2 text-sm">
                <div className="font-mono text-xs text-slate-500">{formatTime(e.timestamp)}</div>
                <div className="font-medium">
                  {e.type} · {e.subtype}
                </div>
                <div className="text-slate-600">
                  {e.broker_name ? `${e.broker_name} · ` : ""}
                  {e.client_ip || ""}
                  {e.adapter_id ? ` · ${e.adapter_id}` : ""}
                  {e.message ? ` · ${e.message}` : ""}
                </div>
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  );
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 border-b border-slate-100 py-1">
      <span className="text-slate-500">{k}</span>
      <span className="font-mono text-right">{v}</span>
    </div>
  );
}
