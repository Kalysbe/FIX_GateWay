import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDate, formatDateTime } from "../labels";
import { dateValue } from "../lib/table";
import { StatusBadge } from "../components/StatusBadge";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner } from "../components/feedback";
import { DataTable, type TableColumn } from "../components/DataTable";
import type { APIToken } from "../types/admin";

export default function TokensPage() {
  const q = useQuery({ queryKey: ["admin-tokens"], queryFn: ({ signal }) => api<{ tokens: APIToken[] }>("/api/v1/admin/tokens", { signal }) });
  const columns: TableColumn<APIToken>[] = [
    { id: "broker", header: "Брокер", value: t => t.broker_name || t.broker_code, cell: t => <><Link to={`/brokers/${t.broker_id}?tab=tokens`} className="font-medium hover:text-accent">{t.broker_name || t.broker_id}</Link><div className="font-mono text-[10px] text-muted">{t.broker_code}</div></> },
    { id: "name", header: "Токен", value: t => t.name, cell: t => <><div className="font-medium">{t.name}</div><div className="font-mono text-[10px] text-muted">{t.token_prefix}…</div></> },
    { id: "status", header: "Статус", value: t => t.status, cell: t => <StatusBadge status={t.status} /> },
    { id: "firm", header: "Фирма", value: t => t.quik_firm, className: "font-mono" },
    { id: "expires", header: "Истекает", value: t => dateValue(t.expires_at), cell: t => t.expires_at ? formatDate(t.expires_at) : "Без срока" },
    { id: "used", header: "Последнее использование", value: t => dateValue(t.last_used_at), cell: t => <><div className="text-[11px]">{t.last_used_at ? formatDateTime(t.last_used_at) : "—"}</div><div className="font-mono text-[10px] text-muted">{t.last_used_ip}</div></> },
    { id: "actions", header: "Действия", value: () => "", sortable: false, cell: t => <Link className="btn-muted" to={`/brokers/${t.broker_id}?tab=tokens`}>Управление</Link> },
  ];
  return <div className="space-y-4"><PageHeader title="Токены" subtitle="Доступ брокеров к API · права и сроки действия" />{q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}<DataTable title="API-токены" rows={q.data?.tokens ?? []} columns={columns} rowKey={t => t.id} loading={q.isLoading} onRefresh={() => void q.refetch()} refreshing={q.isFetching} searchPlaceholder="Брокер, название, фирма, статус…" emptyTitle="Токенов пока нет" emptyHint="Откройте карточку брокера и создайте токен" summary={rows => [{ label: "Токенов", value: rows.length }, { label: "Активны", value: rows.filter(t => t.status === "ACTIVE").length, tone: "success" }, { label: "Отозваны", value: rows.filter(t => t.status === "REVOKED").length, tone: "danger" }, { label: "Истекли", value: rows.filter(t => t.status === "EXPIRED").length, tone: "warn" }]} /></div>;
}
