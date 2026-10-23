import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, formatMs, formatNumber, formatPct, formatRelative, systemKeyLabel } from "../labels";
import { displayAdapterName, isHiddenAdapter } from "../lib/adapters";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, PageSpinner } from "../components/feedback";
import { StatusBadge } from "../components/StatusBadge";
import type { Dashboard } from "../types/admin";
import { GatewayMonitoring } from "../components/GatewayMonitoring";

export default function DashboardPage() {
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["dashboard"],
    staleTime: 5000,
    queryFn: ({ signal }) => api<Dashboard>("/api/v1/admin/dashboard", { signal }),
    refetchInterval: liveRefetchInterval(connected, 5000),
  });

  if (q.isLoading || !q.data) return <div className="space-y-6">
    <PageHeader title="Дашборд" subtitle="Здоровье шлюза и поток ордеров в реальном времени" />
    <GatewayMonitoring />
    {q.error ? <ErrorBanner error={q.error} onRetry={() => q.refetch()} /> : <PageSpinner />}
  </div>;

  const d = q.data!;

  return (
    <div className="space-y-6">
      {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
      {d.stale && <p role="status" className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800">Показана последняя сохранённая статистика. Данные устарели; шлюз повторяет обновление в фоне.</p>}
      <PageHeader title="Дашборд" subtitle="Здоровье шлюза и поток ордеров в реальном времени" />
      {d.generated_at && <p className="text-xs text-muted">Данные на {formatDateTime(d.generated_at)} · статистика обновляется в фоне раз в минуту</p>}
      <div className="overview-hero"><div><span className="login-eyebrow">Операционный обзор</span><h2 className="mt-1">Торговый поток и состояние шлюза</h2><p>Заявки, подключения и ключевые показатели в одном пространстве</p></div><div className="overview-links"><Link className="btn" to="/orders">Книга заявок →</Link><Link className="btn-muted" to="/monitoring">Мониторинг</Link></div></div>
      {d.orders_source_error && <p role="status" className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800">Источник заявок QUIK недоступен: счётчики заявок не показаны. Остальные данные дашборда актуальны.</p>}
      <div className="grid gap-3 md:grid-cols-4">
        <Stat label="Заявок в QUIK" value={d.stats?.total ?? "—"} />
        <Stat label="Исполнено" value={d.stats?.filled ?? "—"} />
        <Stat label="Отклонено" value={d.stats?.rejected ?? "—"} />
        <Stat label="Активных" value={d.stats?.active ?? "—"} />
      </div>
      {d.observability && <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Stat label="Запросов сегодня" value={d.observability.requests_today} />
        <Stat label="Доля успеха" value={formatPct(d.observability.success_rate)} raw />
        <Stat label="Задержка · P95" value={formatMs(d.observability.p95_latency_ms)} raw />
        <Stat label="Брокеров онлайн" value={d.observability.connected_now ?? d.connected_brokers?.length ?? 0} />
      </div>}
      <div className="grid gap-3 md:grid-cols-3">
        {Object.entries(d.system).map(([k, v]) => (
          <div key={k} className="card flex items-center justify-between gap-3">
            <div>
              <div className="text-[13px] text-muted">{systemKeyLabel(k)}</div>
              <div className="mt-2">
                <StatusBadge status={v} />
              </div>
            </div>
            <span
              className={`h-2.5 w-2.5 rounded-full ${
                String(v).toUpperCase() === "ONLINE"
                  ? "bg-emerald-500 shadow-[0_0_10px_rgba(16,185,129,0.85)]"
                  : String(v).toUpperCase() === "DEGRADED"
                    ? "bg-amber-500"
                    : "bg-rose-500"
              }`}
            />
          </div>
        ))}
      </div>
      {d.observability && <div className="dashboard-detail-stats"><span>Ошибки запросов <strong>{formatNumber(d.observability.failed)}</strong></span><span>Критические <strong className="text-danger">{formatNumber(d.observability.critical_today)}</strong></span><span>Средняя задержка <strong>{formatMs(d.observability.avg_latency_ms)}</strong></span><span>P99 <strong>{formatMs(d.observability.p99_latency_ms)}</strong></span><span>Ошибки входа <strong>{formatNumber(d.observability.auth_failures_today)}</strong></span></div>}
      <div className="grid gap-3 md:grid-cols-2">
        <div className="card">
          <h2 className="mb-3 text-[15px] font-semibold tracking-tight">FIX-адаптеры</h2>
          {d.adapters.some(isHiddenAdapter) && <p className="mb-3 text-xs text-muted">Отключены: {d.adapters.filter(isHiddenAdapter).length}. <Link to="/adapters" className="text-accent">Все адаптеры →</Link></p>}
          <div className="space-y-2">
            {d.adapters.filter((a) => !isHiddenAdapter(a)).map((a) => (
              <div key={a.id} className="flex items-center justify-between rounded-md bg-surface px-3 py-2">
                <div>
                  <div className="font-medium">{displayAdapterName(a.name)}</div>
                  <div className="font-mono text-xs text-muted">
                    {a.id} · вес {a.weight}
                  </div>
                </div>
                <div className="text-right">
                  <StatusBadge status={a.status} />
                  <div className="text-xs text-muted">
                    активных {a.active_orders} · готово {a.processed_orders}
                  </div>
                </div>
              </div>
            ))}
            {d.adapters.every(isHiddenAdapter) && <EmptyState title="Нет включённых адаптеров" />}
          </div>
        </div>
        <div className="card">
          <div className="mb-3 flex items-baseline justify-between gap-3">
            <h2 className="text-[15px] font-semibold tracking-tight">Подключённые брокеры</h2>
            <div className="text-xs text-muted">
              {d.connected_brokers?.length || 0} из {d.brokers} · активность за 2 мин
            </div>
          </div>
          <div className="space-y-2">
            {(d.connected_brokers || []).map((b) => (
              <Link
                key={b.broker_id}
                to={`/brokers/${b.broker_id}?tab=activity`}
                className="flex items-center justify-between rounded-md bg-surface px-3 py-2 hover:bg-slate-100"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="h-2 w-2 shrink-0 rounded-full bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.7)]" />
                    <span className="truncate font-medium">{b.broker_name || b.broker_id}</span>
                  </div>
                  <div className="mt-0.5 font-mono text-xs text-muted">
                    {b.broker_code || "—"}
                    {b.last_ip ? ` · ${b.last_ip}` : ""}
                  </div>
                </div>
                <div className="text-right text-xs text-muted">
                  <div>{formatRelative(b.last_activity)}</div>
                  {/* Only write requests are logged; read-only brokers have none. */}
                  {b.requests > 0 && <div>{formatNumber(b.requests)} запр.</div>}
                </div>
              </Link>
            ))}
            {(d.connected_brokers || []).length === 0 && <EmptyState title="Сейчас никто не подключён" />}
          </div>
        </div>
      </div>
      <div className="grid gap-3 lg:grid-cols-2">
        <div className="card overflow-x-auto">
          <h2 className="mb-3 text-[15px] font-semibold tracking-tight">Самые активные брокеры</h2>
          <table className="table">
            <thead>
              <tr>
                <th>Брокер</th>
                <th>Запросы</th>
                <th>Заявки</th>
                <th>Ошибки</th>
                <th>Успех</th>
                <th>Среднее</th>
              </tr>
            </thead>
            <tbody>
              {(d.top_brokers || []).map((b) => (
                <tr key={b.broker_id}>
                  <td>
                    <Link className="hover:text-accent" to={`/brokers/${b.broker_id}?tab=activity`}>
                      {b.broker_name || b.broker_id}
                    </Link>
                  </td>
                  <td>{formatNumber(b.requests)}</td>
                  <td>{formatNumber(b.orders)}</td>
                  <td>{formatNumber(b.errors)}</td>
                  <td>{formatPct(b.success_rate)}</td>
                  <td>{formatMs(b.avg_latency_ms)}</td>
                </tr>
              ))}
              {(d.top_brokers || []).length === 0 && (
                <tr>
                  <td colSpan={6}>
                    <EmptyState title="Пока нет API-активности" />
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <div className="card overflow-x-auto">
          <h2 className="mb-3 text-[15px] font-semibold tracking-tight">Топ ошибок</h2>
          <table className="table">
            <thead>
              <tr>
                <th>Ошибка</th>
                <th>Кол-во</th>
                <th>Последняя</th>
                <th>Брокеры</th>
              </tr>
            </thead>
            <tbody>
              {(d.top_errors || []).map((e) => (
                <tr key={e.error}>
                  <td>
                    <Link className="font-mono text-xs hover:text-accent" to={`/logs/errors?error_code=${encodeURIComponent(e.error)}`}>
                      {e.error}
                    </Link>
                  </td>
                  <td>{formatNumber(e.count)}</td>
                  <td className="text-xs text-muted">{formatDateTime(e.last_occurrence)}</td>
                  <td>{e.affected_brokers}</td>
                </tr>
              ))}
              {(d.top_errors || []).length === 0 && (
                <tr>
                  <td colSpan={4}>
                    <EmptyState title="Ошибок за сегодня нет" />
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
      <details className="dashboard-runtime"><summary><span>Телеметрия и путь заявки</span><span>Нагрузка сервера · графики · FIX-сессии</span></summary><div className="mt-4"><GatewayMonitoring dashboard={d} /></div></details>
    </div>
  );
}

function Stat({ label, value, raw }: { label: string; value: number | string; raw?: boolean }) {
  return (
    <div className={"card metric-card " + (/Отклонено|Ошиб/.test(label) ? "metric-card-danger" : /Исполнено|успеха/.test(label) ? "metric-card-success" : "")} >
      <div className="text-[11px] text-muted">{label}</div>
      <div className="mt-1.5 font-mono text-[1.7rem] font-medium tracking-tight">
        {raw ? value : formatNumber(Number(value) || 0)}
      </div>
    </div>
  );
}
