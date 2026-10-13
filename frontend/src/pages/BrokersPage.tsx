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
import { ErrorBanner } from "../components/feedback";
import { DataTable, type TableColumn } from "../components/DataTable";
import { dateValue } from "../lib/table";
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

  const columns: TableColumn<Broker>[] = [
    { id: "id", header: "ID", value: b => b.id, numeric: true },
    { id: "code", header: "Код", value: b => b.code, className: "font-mono" },
    { id: "name", header: "Брокер", value: b => b.name, cell: b => <Link to={"/brokers/" + b.id + "?tab=tokens"} className="font-medium hover:text-accent">{b.name}</Link> },
    { id: "connected", header: "Подключение", value: b => b.connected ? "Подключён" : "Нет соединения", cell: b => <><StatusBadge status={b.connected ? "CONNECTED" : "OFFLINE"} />{b.connected && <div className="mt-1 text-[10px] text-muted">{formatRelative(b.last_activity)}{b.last_ip ? " · " + b.last_ip : ""}</div>}</> },
    { id: "status", header: "Статус", value: b => b.status, cell: b => <StatusBadge status={b.status} /> },
    { id: "created", header: "Создан", value: b => dateValue(b.created_at), cell: b => formatDateTime(b.created_at), className: "text-[10px] whitespace-nowrap" },
    { id: "actions", header: "Действия", value: () => "", sortable: false, cell: b => <div className="flex gap-1.5"><Link className="btn-muted" to={"/brokers/" + b.id + "?tab=tokens"}>Открыть</Link><button type="button" className="btn-muted" onClick={() => setStatus(b.id, b.status === "ACTIVE" ? "BLOCKED" : "ACTIVE")}>{b.status === "ACTIVE" ? "Заблокировать" : "Активировать"}</button></div> },
  ];

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
      <DataTable title="Брокеры" rows={q.data?.brokers ?? []} columns={columns} rowKey={b => b.id} loading={q.isLoading} searchPlaceholder="Название, код, статус…" onRefresh={() => void q.refetch()} refreshing={q.isFetching} emptyTitle="Брокеров пока нет" summary={rows => [{ label: "Брокеров", value: rows.length }, { label: "Подключены", value: rows.filter(b => b.connected).length, tone: "success" }, { label: "Активны", value: rows.filter(b => b.status === "ACTIVE").length }, { label: "Заблокированы", value: rows.filter(b => b.status === "BLOCKED").length, tone: "danger" }]} />

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
