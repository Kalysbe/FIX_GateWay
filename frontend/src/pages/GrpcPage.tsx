import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, getSessionUser } from "../api";
import { useAdminStream } from "../hooks/useAdminStream";
import { useBreadcrumbs } from "../components/breadcrumbs";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable, type TableColumn } from "../components/DataTable";

type Connection = { id: string; user_id: string; email: string; role: string; broker_id?: number; broker_code?: string; broker_name?: string; transport: string; peer: string; connected_at: string; topics: string[]; paths: string[] };
type Snapshot = { instance_id: string; revision: number; connections: Connection[] };
export default function GrpcPage() {
  useBreadcrumbs([{ label: "gRPC" }]);
  const { connected, subscribe } = useAdminStream();
  const [snapshot, setSnapshot] = useState<Snapshot>();

  const allowed = ["SUPER_ADMIN", "ADMIN", "OPERATOR", "AUDITOR"].includes(getSessionUser()?.role ?? "");
  const query = useQuery({ queryKey: ["grpc-connections"], queryFn: ({ signal }) => api<Snapshot>("/api/v1/admin/grpc/connections", { signal }), enabled: allowed, refetchInterval: 10000 });
  useEffect(() => { if (query.data) setSnapshot(old => !old || query.data!.instance_id !== old.instance_id || query.data!.revision >= old.revision ? query.data : old); }, [query.data]);
  useEffect(() => subscribe(msg => { if (msg.type === "subscriptions.updated") { const next = msg.data as Snapshot; setSnapshot(old => !old || next.instance_id !== old.instance_id || next.revision >= old.revision ? next : old); } }), [subscribe]);
  if (!allowed) return <p>Просмотр gRPC-подключений недоступен для вашей роли.</p>;
  const all = (snapshot?.connections ?? []).filter(c => c.transport === "gRPC");
  const columns: TableColumn<Connection>[] = [
    { id: "client", header: "Клиент", value: c => [c.broker_name, c.broker_code, c.email, c.user_id].filter(Boolean).join(" "), cell: c => <>{c.broker_id ? <Link className="text-accent font-medium" to={"/brokers/" + c.broker_id}>{c.broker_name} · {c.broker_code}</Link> : c.email || c.user_id}<div className="text-[10px] text-muted">{c.role}</div></> },
    { id: "transport", header: "Подключение", value: c => c.transport + " " + c.peer, cell: c => <><span className="badge-success">gRPC Stream</span><div className="mt-1 font-mono text-[10px] text-muted">{c.peer}</div></> },
    { id: "topics", header: "Подписки", value: c => c.topics.join(" "), cell: c => <div className="space-y-1">{c.topics.map(t => <code className="block text-[10px]" key={t}>{t}</code>)}</div> },
    { id: "paths", header: "Пути данных", value: c => c.paths.join(" "), cell: c => <div className="space-y-1">{c.paths.map(p => <code className="block text-[10px]" key={p}>{p}</code>)}</div> },
    { id: "created", header: "Подключён с", value: c => Date.parse(c.connected_at), cell: c => new Date(c.connected_at).toLocaleString("ru-RU"), className: "whitespace-nowrap text-[10px]" },
  ];
  return <div className="space-y-5">
    <PageHeader title="gRPC · Подписки" subtitle="Подключения внешних систем и их подписки" actions={<span className={connected ? "text-emerald-700" : "text-amber-700"}>{connected ? "● В реальном времени" : "Соединение восстанавливается"}</span>}/>
    {query.error && <ErrorBanner error={query.error} onRetry={() => query.refetch()}/>}
    <p className="text-xs text-muted">Подключений внешних систем: {all.length}</p>
    <DataTable title="gRPC-подписки" rows={all} columns={columns} rowKey={c => c.id} loading={query.isLoading} searchPlaceholder="Брокер, путь данных, событие…" onRefresh={() => void query.refetch()} refreshing={query.isFetching} emptyTitle="Активных подписок нет" summary={rows => [{ label: "Подключений", value: rows.length }, { label: "Тем событий", value: new Set(rows.flatMap(c => c.topics)).size }, { label: "Путей данных", value: new Set(rows.flatMap(c => c.paths)).size }]} />
    <p className="text-sm text-slate-500">Подключения нашей админки скрыты. Все показанные подписки используют gRPC Stream. Пути показывают источник данных события; обычные HTTP-запросы здесь не отображаются. После отключения клиент исчезает из списка.</p>
  </div>;
}
