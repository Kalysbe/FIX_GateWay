import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, formatNumber } from "../labels";
import { useBreadcrumbs } from "../components/breadcrumbs";
import { PageHeader } from "../components/PageHeader";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";

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

function tone(sev: string) {
  switch (sev) {
    case "CRITICAL":
      return "border-rose-300 bg-rose-50";
    case "ERROR":
      return "border-rose-100 bg-rose-50/60";
    case "WARNING":
      return "border-amber-100 bg-amber-50";
    default:
      return "border-line bg-panel";
  }
}

export default function NotificationsPage() {
  useBreadcrumbs([{ label: "Уведомления" }]);
  const qc = useQueryClient();
  const { connected } = useAdminStream();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const q = useQuery({
    queryKey: ["notifications", "list", page, pageSize],
    queryFn: ({ signal }) =>
      api<NotesPage>(`/api/v1/admin/notifications?limit=${pageSize}&page=${page}`, { signal }),
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
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);

  useEffect(() => {
    if (page > pages) setPage(pages);
  }, [page, pages]);

  return (
    <div className="space-y-4">
      <PageHeader
        title="Уведомления"
        subtitle={`Непрочитанных: ${formatNumber(q.data?.unread ?? 0)}. Одинаковые ошибки агрегируются за минуту.`}
        actions={
          <button type="button" className="btn-muted" onClick={() => readAll.mutate()} disabled={readAll.isPending}>
            Прочитать все
          </button>
        }
      />
      <div className="space-y-2">
        {items.map((n) => (
          <div key={n.id} className={`card border ${tone(n.severity)} ${n.severity === "CRITICAL" ? "ring-1 ring-rose-300" : ""}`}>
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <span className={n.severity === "CRITICAL" || n.severity === "ERROR" ? "badge-danger" : n.severity === "WARNING" ? "badge-warn" : "badge-info"}>
                    {n.severity}
                  </span>
                  <span className="font-medium">{n.title}</span>
                  {n.count > 1 && <span className="badge-muted">×{n.count}</span>}
                </div>
                <p className="mt-1 text-sm text-slate-600">{n.message || n.subtype}</p>
                <p className="mt-1 text-xs text-slate-400">
                  {formatDateTime(n.last_seen_at)}
                  {n.broker_name ? ` · ${n.broker_name}` : ""}
                </p>
              </div>
              <div className="flex gap-2">
                <Link className="btn-muted" to={`/logs/errors?${n.error_code ? `error_code=${n.error_code}` : ""}`}>
                  Логи
                </Link>
                {!n.read_at && (
                  <button type="button" className="btn-muted" onClick={() => readOne.mutate(n.id)}>
                    Прочитано
                  </button>
                )}
              </div>
            </div>
          </div>
        ))}
        {items.length === 0 && <div className="card text-sm text-slate-400">{q.isLoading ? "Загрузка…" : "Нет уведомлений"}</div>}
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
        <div className="text-slate-500">
          Показано <span className="font-mono text-ink">{from}–{to}</span> из{" "}
          <span className="font-mono text-ink">{formatNumber(total)}</span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex items-center gap-2 text-slate-500">
            На странице
            <select
              className="input w-auto py-1"
              value={pageSize}
              onChange={(e) => {
                setPageSize(Number(e.target.value));
                setPage(1);
              }}
            >
              {[10, 20].map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="btn-muted px-2 py-1" disabled={page <= 1} onClick={() => setPage(1)}>
            «
          </button>
          <button type="button" className="btn-muted px-2 py-1" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>
            Назад
          </button>
          <span className="tabular-nums text-slate-500">
            {page} / {pages}
          </span>
          <button
            type="button"
            className="btn-muted px-2 py-1"
            disabled={page >= pages}
            onClick={() => setPage((p) => p + 1)}
          >
            Далее
          </button>
        </div>
      </div>
    </div>
  );
}
