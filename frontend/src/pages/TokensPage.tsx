import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDate, formatDateTime } from "../labels";
import { StatusBadge } from "../components/StatusBadge";
import { Icons } from "../components/icons";

type Broker = { id: number; code: string; name: string; status: string };

type Token = {
  id: string;
  name: string;
  status: string;
  token_prefix: string;
  quik_firm?: string;
  permissions: string[];
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  last_used_ip?: string;
};

type Row = Token & { broker_id: number; broker_name: string; broker_code: string };

export default function TokensPage() {
  const brokers = useQuery({
    queryKey: ["brokers"],
    queryFn: () => api<{ brokers: Broker[] }>("/api/v1/admin/brokers"),
  });

  const list = brokers.data?.brokers || [];

  const tokenQueries = useQueries({
    queries: list.map((b) => ({
      queryKey: ["tokens", String(b.id)],
      queryFn: () => api<{ tokens: Token[] }>(`/api/v1/admin/brokers/${b.id}/tokens`),
      enabled: list.length > 0,
    })),
  });

  const loading = brokers.isLoading || tokenQueries.some((q) => q.isLoading);

  const rows: Row[] = list.flatMap((b, i) =>
    (tokenQueries[i]?.data?.tokens || []).map((t) => ({
      ...t,
      broker_id: b.id,
      broker_name: b.name,
      broker_code: b.code,
    })),
  );

  const active = rows.filter((r) => r.status === "ACTIVE").length;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Токены</h1>
          <p className="text-sm text-slate-500">API-токены всех брокеров</p>
        </div>
        <div className="flex gap-2 text-xs">
          <div className="rounded-lg border border-slate-200 bg-white px-3 py-1.5">
            <span className="mr-2 text-slate-500">Всего</span>
            <span className="font-mono font-semibold">{rows.length}</span>
          </div>
          <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-emerald-800">
            <span className="mr-2 text-emerald-700/80">Активных</span>
            <span className="font-mono font-semibold">{active}</span>
          </div>
        </div>
      </div>

      <div className="card !p-0 overflow-hidden">
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
            {loading && (
              <tr>
                <td colSpan={7} className="py-10 text-center text-slate-400">
                  Загрузка…
                </td>
              </tr>
            )}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={7} className="py-10 text-center text-slate-400">
                  Токенов пока нет. Откройте брокера и создайте токен.
                </td>
              </tr>
            )}
            {!loading &&
              rows.map((t) => (
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
                  <td className="text-sm text-slate-600">
                    {t.expires_at ? formatDate(t.expires_at) : "Без срока"}
                  </td>
                  <td>
                    <div className="text-sm">{t.last_used_at ? formatDateTime(t.last_used_at) : "—"}</div>
                    {t.last_used_ip && (
                      <div className="font-mono text-[11px] text-slate-400">{t.last_used_ip}</div>
                    )}
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
      </div>
    </div>
  );
}
