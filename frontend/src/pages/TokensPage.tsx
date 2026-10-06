import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDate, formatDateTime } from "../labels";
import { StatusBadge } from "../components/StatusBadge";
import { Icons } from "../components/icons";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, TableSkeleton } from "../components/feedback";
import type { APIToken } from "../types/admin";

export default function TokensPage() {
  const q = useQuery({
    queryKey: ["admin-tokens"],
    queryFn: ({ signal }) => api<{ tokens: APIToken[] }>("/api/v1/admin/tokens", { signal }),
  });

  const rows = q.data?.tokens || [];
  const active = rows.filter((r) => r.status === "ACTIVE").length;

  return (
    <div className="space-y-4">
      <PageHeader
        title="Токены"
        subtitle="API-токены всех брокеров"
        actions={
          <div className="flex gap-2 text-xs">
            <div className="rounded-lg border border-line bg-panel px-3 py-1.5">
              <span className="mr-2 text-slate-500">Всего</span>
              <span className="font-mono font-semibold">{rows.length}</span>
            </div>
            <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-emerald-800">
              <span className="mr-2 text-emerald-700/80">Активных</span>
              <span className="font-mono font-semibold">{active}</span>
            </div>
          </div>
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
                <th>Брокер</th>
                <th>Название</th>
                <th>Статус</th>
                <th>Фирма</th>
                <th>Истекает</th>
                <th>Последнее использование</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 && (
                <tr>
                  <td colSpan={7}>
                    <EmptyState title="Токенов пока нет" hint="Откройте брокера и создайте токен" />
                  </td>
                </tr>
              )}
              {rows.map((t) => (
                <tr key={t.id}>
                  <td>
                    <Link to={`/brokers/${t.broker_id}?tab=tokens`} className="font-medium hover:text-accent">
                      {t.broker_name}
                    </Link>
                    <div className="font-mono text-[11px] text-slate-400">{t.broker_code}</div>
                  </td>
                  <td>
                    <div className="font-medium">{t.name}</div>
                    <div className="font-mono text-[11px] text-slate-400">{t.token_prefix}…</div>
                  </td>
                  <td>
                    <StatusBadge status={t.status} />
                  </td>
                  <td className="font-mono text-xs">{t.quik_firm || "—"}</td>
                  <td className="text-sm text-slate-600">{t.expires_at ? formatDate(t.expires_at) : "Без срока"}</td>
                  <td>
                    <div className="text-sm">{t.last_used_at ? formatDateTime(t.last_used_at) : "—"}</div>
                    {t.last_used_ip && <div className="font-mono text-[11px] text-slate-400">{t.last_used_ip}</div>}
                  </td>
                  <td>
                    <Link className="btn-muted gap-1" to={`/brokers/${t.broker_id}?tab=tokens`}>
                      <Icons.tokens size={14} />
                      Открыть
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
