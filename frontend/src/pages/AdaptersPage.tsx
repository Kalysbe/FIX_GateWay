import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { displayAdapterName, normalizeAdapter, isSimulator } from "../lib/adapters";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, PageSpinner } from "../components/feedback";
import { StatusBadge } from "../components/StatusBadge";
import { Icons } from "../components/icons";
import { DataTable, type TableColumn } from "../components/DataTable";
import type { Adapter } from "../types/admin";
import type { NormalizedAdapter } from "../lib/adapters";

export default function AdaptersPage() {
  const { connected } = useAdminStream();
  const q = useQuery({ queryKey: ["adapters"], queryFn: ({ signal }) => api<{ adapters: Adapter[] }>("/api/v1/admin/adapters", { signal }), refetchInterval: liveRefetchInterval(connected, 3000) });
  const adapters = (q.data?.adapters ?? []).map(normalizeAdapter);
  const columns: TableColumn<NormalizedAdapter>[] = [
    { id: "name", header: "Соединение", value: a => displayAdapterName(a.name) },
    { id: "id", header: "ID", value: a => a.id, className: "font-mono" },
    { id: "type", header: "Тип", value: a => a.type },
    { id: "address", header: "Адрес", value: a => a.host + ":" + a.port, className: "font-mono text-[10px]" },
    { id: "status", header: "Состояние", value: a => a.enabled ? a.status : "DISABLED", cell: a => <StatusBadge status={a.enabled ? a.status : "DISABLED"} /> },
    { id: "weight", header: "Вес", value: a => a.weight, numeric: true },
    { id: "active", header: "Активные", value: a => a.active_orders, numeric: true },
    { id: "processed", header: "Обработано", value: a => a.processed_orders, numeric: true },
    { id: "errors", header: "Ошибки", value: a => a.error_count, numeric: true, cell: a => <span className={a.error_count ? "text-danger" : "text-muted"}>{a.error_count}</span> },
  ];
  return <div className="space-y-4"><PageHeader title="Подключения FIX / QUIK" subtitle="Состояние торговых соединений · маршрутизация и нагрузка" />{q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}{q.isLoading && <PageSpinner />}
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">{adapters.filter(a => a.enabled).map(a => <section key={a.id} className="card adapter-card"><div className="adapter-card-header"><div><div className="mb-2 flex items-center gap-2 text-[9px] uppercase tracking-wider text-muted"><Icons.adapters size={14} />{isSimulator(a) ? "Тестовый контур" : "QUIK / FIX"}</div><h2>{displayAdapterName(a.name)}</h2><p className="adapter-address">{a.host}:{a.port} · {a.id}</p></div><StatusBadge status={a.status} /></div><div className="adapter-metrics"><div><span>Активные заявки</span><strong>{a.active_orders.toLocaleString("ru-RU")}</strong></div><div><span>Обработано</span><strong>{a.processed_orders.toLocaleString("ru-RU")}</strong></div><div><span>Ошибки</span><strong className={a.error_count ? "text-danger" : "text-muted"}>{a.error_count.toLocaleString("ru-RU")}</strong></div></div></section>)}</div>
    {!q.isLoading && !adapters.length && <EmptyState title="Подключения не настроены" />}
    <DataTable title="Все FIX-адаптеры" rows={adapters} columns={columns} rowKey={a => a.id} loading={q.isLoading} onRefresh={() => void q.refetch()} refreshing={q.isFetching} summary={rows => [{ label: "Всего", value: rows.length }, { label: "Онлайн", value: rows.filter(a => a.enabled && a.status === "ONLINE").length, tone: "success" }, { label: "Отключены", value: rows.filter(a => !a.enabled).length }, { label: "Активных заявок", value: rows.reduce((n, a) => n + a.active_orders, 0) }]} />
  </div>;
}
