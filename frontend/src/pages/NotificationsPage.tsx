import { useEffect } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, formatNumber } from "../labels";
import { useBreadcrumbs } from "../components/breadcrumbs";
import { PageHeader } from "../components/PageHeader";
import { liveRefetchInterval } from "../lib/poll";
import { dateValue } from "../lib/table";
import { useUrlPatch, useUrlState } from "../lib/urlState";
import { useAdminStream } from "../hooks/useAdminStream";
import { ErrorBanner } from "../components/feedback";
import { StatusBadge } from "../components/StatusBadge";
import { ActionButton } from "../components/ActionButton";
import { DataTable, type TableColumn } from "../components/DataTable";

type Note = {
  id: string;
  severity: string;
  event_type: string;
  subtype: string;
  error_code?: string;
  broker_id?: number | null;
  broker_name?: string;
  title: string;
  message: string;
  count: number;
  last_seen_at: string;
  read_at?: string | null;
};

type NotesPage = {
  notifications: Note[];
  unread: number;
  total: number;
  limit: number;
  page: number;
  has_more: boolean;
};

const PAGE_SIZE = 20;
const SEVERITY_ORDER: Record<string, number> = { CRITICAL: 4, ERROR: 3, WARNING: 2, INFO: 1, DEBUG: 0 };
const logsLink = (n: Note) => `/logs/errors?${n.error_code ? `error_code=${encodeURIComponent(n.error_code)}` : ""}`;

export default function NotificationsPage() {
  useBreadcrumbs([{ label: "Уведомления" }]);
  const qc = useQueryClient();
  const { connected } = useAdminStream();
  const [pageText] = useUrlState("npage", "1");
  const [unreadText] = useUrlState("unread");
  const patch = useUrlPatch();
  const page = Math.max(1, Number(pageText) || 1);
  const unreadOnly = unreadText === "1";
  const setPage = (p: number) => patch({ npage: p > 1 ? String(p) : null });
  const q = useQuery({
    queryKey: ["notifications", "list", page, PAGE_SIZE, unreadOnly],
    queryFn: ({ signal }) =>
      api<NotesPage>(`/api/v1/admin/notifications?limit=${PAGE_SIZE}&page=${page}${unreadOnly ? "&unread=1" : ""}`, { signal }),
    placeholderData: keepPreviousData,
    refetchInterval: liveRefetchInterval(connected, 8000),
  });
  const readAll = useMutation({
    mutationFn: () => api("/api/v1/admin/notifications/read-all", { method: "POST" }),
    onSuccess: () => {
      setPage(1);
      qc.invalidateQueries({ queryKey: ["notifications"] });
    },
  });
  const readOne = useMutation({
    mutationFn: (id: string) => api(`/api/v1/admin/notifications/${id}/read`, { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"] }),
  });

  const items = q.data?.notifications || [];
  const total = q.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  useEffect(() => {
    if (q.data && !q.isPlaceholderData && page > pages) setPage(pages);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pages, q.data, q.isPlaceholderData]);

  const columns: TableColumn<Note>[] = [
    { id: "severity", header: "Важность", value: n => SEVERITY_ORDER[n.severity] ?? -1, exportValue: n => n.severity, cell: n => <StatusBadge status={n.severity} /> },
    { id: "title", header: "Событие", value: n => n.title, cell: n => <div className="min-w-[220px] max-w-[560px]">
      <div className="flex flex-wrap items-center gap-2"><span className="font-medium">{n.title}</span>{!n.read_at && <span className="notification-new">Новое</span>}{n.count > 1 && <span className="badge-muted">×{n.count}</span>}</div>
      <div className="mt-0.5 whitespace-normal text-[11px] text-muted">{n.message || n.subtype}</div>
    </div> },
    { id: "broker", header: "Брокер", value: n => n.broker_name ?? "", cell: n => n.broker_name || "—" },
    { id: "code", header: "Код", value: n => n.error_code ?? n.subtype, className: "font-mono text-[11px]", defaultHidden: true },
    { id: "count", header: "Повторов", value: n => n.count, numeric: true, defaultHidden: true },
    { id: "seen", header: "Последнее", value: n => dateValue(n.last_seen_at), exportValue: n => n.last_seen_at, cell: n => formatDateTime(n.last_seen_at), className: "whitespace-nowrap text-[11px]" },
    { id: "actions", header: "", value: () => "", sortable: false, cell: n => <div className="flex justify-end gap-1.5">
      <Link className="btn-muted" to={logsLink(n)}>Логи</Link>
      {!n.read_at && <ActionButton onAction={() => readOne.mutateAsync(n.id)} failure="Не удалось отметить уведомление">Прочитано</ActionButton>}
    </div> },
  ];

  return (
    <div className="space-y-4">
      <PageHeader
        title="Уведомления"
        subtitle="События системы и торгового потока · повторяющиеся ошибки объединяются"
        actions={
          <ActionButton onAction={() => readAll.mutateAsync()} disabled={!q.data?.unread} success="Все уведомления отмечены прочитанными" failure="Не удалось отметить уведомления">
            Прочитать все
          </ActionButton>
        }
      />
      {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
      <DataTable title="Уведомления" rows={q.error ? [] : items} columns={columns} rowKey={n => n.id} loading={q.isLoading} showTotals={false} urlState="n" resetKey={`${unreadOnly}:${page}`}
        rowClassName={n => n.read_at ? "" : `notification-row-unread notification-row-${n.severity.toLowerCase()}`}
        onRefresh={() => void q.refetch()} refreshing={q.isFetching} exportName="notifications" searchPlaceholder="Поиск по загруженной странице…"
        emptyTitle={unreadOnly ? "Все уведомления прочитаны" : "Нет уведомлений"}
        filters={<div className="flex gap-1">
          <button type="button" className={unreadOnly ? "btn-muted" : "btn"} aria-pressed={!unreadOnly} onClick={() => patch({ unread: null, npage: null })}>Все события</button>
          <button type="button" className={unreadOnly ? "btn" : "btn-muted"} aria-pressed={unreadOnly} onClick={() => patch({ unread: "1", npage: null })}>Непрочитанные · {formatNumber(q.data?.unread ?? 0)}</button>
        </div>}
        selection={{
          canSelect: n => !n.read_at,
          actions: [{
            label: "Отметить прочитанными",
            run: async list => { for (const n of list) await api(`/api/v1/admin/notifications/${n.id}/read`, { method: "POST" }); await qc.invalidateQueries({ queryKey: ["notifications"] }); },
            success: list => `Отмечено прочитанными: ${list.length}`,
          }],
        }}
        serverPage={{ start: total === 0 ? 1 : (page - 1) * PAGE_SIZE + 1, end: total === 0 ? 0 : Math.min(page * PAGE_SIZE, total), size: PAGE_SIZE, hasPrevious: page > 1, hasNext: page < pages, busy: q.isFetching, onPrevious: () => setPage(page - 1), onNext: () => setPage(page + 1) }} />
    </div>
  );
}
