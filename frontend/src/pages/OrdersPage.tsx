import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, sideLabel } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, TableSkeleton } from "../components/feedback";
import { StatusBadge } from "../components/StatusBadge";
import type { AdminOrder, Broker } from "../types/admin";
import { ReconciliationQueue } from "../components/ReconciliationQueue";

export default function OrdersPage() {
  const { connected } = useAdminStream();
  const [status, setStatus] = useState("");
  const [brokerId, setBrokerId] = useState("");
  const q = useQuery({
    queryKey: ["admin-orders"],
    queryFn: ({ signal }) => api<{ orders: AdminOrder[] }>("/api/v1/admin/orders", { signal }),
    refetchInterval: liveRefetchInterval(connected, 3000),
  });
  const brokers = useQuery({
    queryKey: ["brokers"],
    queryFn: ({ signal }) => api<{ brokers: Broker[] }>("/api/v1/admin/brokers", { signal }),
    staleTime: 60_000,
  });

  const orders = q.data?.orders || [];
  const statuses = useMemo(() => Array.from(new Set(orders.map((o) => o.status).filter(Boolean))) as string[], [orders]);
  const filtered = orders.filter((o) => {
    if (status && o.status !== status) return false;
    if (brokerId && String(o.broker_id) !== brokerId) return false;
    return true;
  });

  return (
    <div className="space-y-6">
      <PageHeader title="Заявки" subtitle="Книга ордеров шлюза по всем брокерам" />
      <ReconciliationQueue />
      {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
      <div className="flex flex-wrap gap-2">
        <select className="input w-auto" value={brokerId} onChange={(e) => setBrokerId(e.target.value)}>
          <option value="">Все брокеры</option>
          {(brokers.data?.brokers || []).map((b) => (
            <option key={b.id} value={b.id}>
              {b.name}
            </option>
          ))}
        </select>
        <select className="input w-auto" value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">Все статусы</option>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>
      <div className="card overflow-x-auto !p-0">
        {q.isLoading ? (
          <TableSkeleton cols={8} />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Брокер</th>
                <th>Фирма</th>
                <th>Код клиента</th>
                <th>Инструмент</th>
                <th>Сторона</th>
                <th>Кол-во</th>
                <th>Цена</th>
                <th>Статус</th>
                <th>Адаптер</th>
                <th>Создано</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((o) => (
                <tr key={o.id}>
                  <td>
                    <Link className="text-accent hover:underline" to={`/orders/${o.id}`}>
                      {String(o.id).slice(0, 8)}
                    </Link>
                  </td>
                  <td>{o.broker_id}</td>
                  <td>
                    <div>{o.firm_name || "—"}</div>
                    {o.quik_firm ? <div className="font-mono text-xs text-slate-500">{o.quik_firm}</div> : null}
                  </td>
                  <td className="font-mono text-xs">{o.client_code || "—"}</td>
                  <td>{o.symbol}</td>
                  <td>{sideLabel(o.side)}</td>
                  <td>{o.quantity}</td>
                  <td>{o.price ?? "—"}</td>
                  <td>
                    <StatusBadge status={o.status} />
                  </td>
                  <td>{o.adapter_id || "—"}</td>
                  <td>{formatDateTime(o.created_at)}</td>
                </tr>
              ))}
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={11}>
                    <EmptyState title="Заявок нет" hint="Новые ордера появятся после отправки брокером" />
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
