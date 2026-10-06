import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, formatRelative } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { useToast } from "../components/Toast";
import { StatusBadge } from "../components/StatusBadge";
import { Icons } from "../components/icons";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, TableSkeleton } from "../components/feedback";
import { Modal } from "../components/Modal";
import type { Broker } from "../types/admin";

export default function BrokersPage() {
  const qc = useQueryClient();
  const toast = useToast();
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["brokers"],
    queryFn: ({ signal }) => api<{ brokers: Broker[] }>("/api/v1/admin/brokers", { signal }),
    refetchInterval: liveRefetchInterval(connected, 5000),
  });
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const create = useMutation({
    mutationFn: () => api("/api/v1/admin/brokers", { method: "POST", body: JSON.stringify({ code, name, description: "" }) }),
    onSuccess: () => {
      setCode("");
      setName("");
      setShowCreate(false);
      qc.invalidateQueries({ queryKey: ["brokers"] });
    },
    onError: (err) => toast.push(err instanceof Error ? err.message : "Не удалось создать брокера"),
  });

  async function setStatus(id: number, status: string) {
    try {
      await api(`/api/v1/admin/brokers/${id}`, { method: "PATCH", body: JSON.stringify({ status }) });
      qc.invalidateQueries({ queryKey: ["brokers"] });
    } catch (err) {
      toast.push(err instanceof Error ? err.message : "Не удалось изменить статус");
    }
  }

  function onCreate(e: FormEvent) {
    e.preventDefault();
    create.mutate();
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="Брокеры"
        subtitle="Управление брокерскими компаниями и доступом"
        actions={
          <button type="button" className="btn gap-1.5" onClick={() => setShowCreate(true)}>
            <Icons.plus size={16} />
            Создать брокера
          </button>
        }
      />
      {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
      <div className="card !p-0 overflow-hidden">
        {q.isLoading ? (
          <TableSkeleton cols={6} />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Код</th>
                <th>Название</th>
                <th>Подключение</th>
                <th>Статус</th>
                <th>Создан</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {(q.data?.brokers || []).map((b) => (
                <tr key={b.id}>
                  <td className="text-slate-500">{b.id}</td>
                  <td className="font-mono text-sm">{b.code}</td>
                  <td>
                    <Link to={`/brokers/${b.id}?tab=tokens`} className="font-medium text-ink hover:text-accent">
                      {b.name}
                    </Link>
                  </td>
                  <td>
                    {b.connected ? (
                      <div>
                        <StatusBadge status="CONNECTED" />
                        <div className="mt-0.5 text-xs text-slate-500">
                          {formatRelative(b.last_activity)}
                          {b.last_ip ? ` · ${b.last_ip}` : ""}
                        </div>
                      </div>
                    ) : (
                      <StatusBadge status="OFFLINE" />
                    )}
                  </td>
                  <td>
                    <StatusBadge status={b.status} />
                  </td>
                  <td className="text-slate-500">{formatDateTime(b.created_at)}</td>
                  <td className="space-x-2 whitespace-nowrap">
                    <Link className="btn-muted" to={`/brokers/${b.id}?tab=tokens`}>
                      Открыть
                    </Link>
                    {b.status === "ACTIVE" ? (
                      <button type="button" className="btn-muted" onClick={() => setStatus(b.id, "BLOCKED")}>
                        Заблокировать
                      </button>
                    ) : (
                      <button type="button" className="btn-muted" onClick={() => setStatus(b.id, "ACTIVE")}>
                        Активировать
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {(q.data?.brokers || []).length === 0 && (
                <tr>
                  <td colSpan={7}>
                    <EmptyState title="Брокеров пока нет" />
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      {showCreate && (
        <Modal title="Создать брокера" onClose={() => setShowCreate(false)}>
          <form onSubmit={onCreate} className="space-y-3">
            <input className="input" placeholder="Код" value={code} onChange={(e) => setCode(e.target.value)} required />
            <input className="input" placeholder="Название" value={name} onChange={(e) => setName(e.target.value)} required />
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-muted" onClick={() => setShowCreate(false)}>
                Отмена
              </button>
              <button className="btn" type="submit" disabled={create.isPending}>
                Создать
              </button>
            </div>
          </form>
        </Modal>
      )}
    </div>
  );
}
