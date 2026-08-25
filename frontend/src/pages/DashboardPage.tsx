import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { Link } from "react-router-dom";
import { api, getToken, isSessionValid, redirectToLogin } from "../api";
import { formatDateTime, formatMs, formatNumber, formatPct, formatRelative, statusLabel, systemKeyLabel } from "../labels";

function isSimulator(a: { id?: string; name?: string; type?: string }): boolean {
  const type = String(a.type || "").toLowerCase();
  const id = String(a.id || "").toLowerCase();
  const name = String(a.name || "").toLowerCase();
  return type === "simulator" || id.includes("simulator") || name.includes("simulator");
}

function isHiddenAdapter(a: { id?: string; name?: string; type?: string }): boolean {
  const id = String(a.id || "").toLowerCase();
  const name = String(a.name || "").toLowerCase();
  return (
    isSimulator(a) ||
    id === "quik-fix-1b" ||
    (id.endsWith("b") && id.startsWith("quik-fix-")) ||
    name.includes("secondary") ||
    name.includes("резервный")
  );
}

function displayName(name: string): string {
  return name.replace(/\bprimary\b/gi, "основной");
}

type Dashboard = {
  system: { api: string; database: string; queue: string };
  adapters: Array<{
    id: string;
    name: string;
    status: string;
    active_orders: number;
    processed_orders: number;
    error_count: number;
    weight: number;
  }>;
  stats: { total: number; filled: number; rejected: number; active: number };
  brokers: number;
  active_accounts: number;
  adapters_available: number;
  observability?: {
    requests_today: number;
    orders_today: number;
    successful: number;
    failed: number;
    success_rate: number;
    avg_latency_ms: number;
    p95_latency_ms: number;
    p99_latency_ms: number;
    errors_today: number;
    critical_today: number;
    auth_failures_today: number;
    active_brokers: number;
    connected_now?: number;
  };
  connected_brokers?: Array<{
    broker_id: number;
    broker_code: string;
    broker_name: string;
    last_activity: string;
    last_ip?: string;
    requests: number;
  }>;
  top_brokers?: Array<{
    broker_id: number;
    broker_name: string;
    requests: number;
    orders: number;
    errors: number;
    success_rate: number;
    avg_latency_ms: number;
    last_activity?: string;
  }>;
  top_errors?: Array<{
    error: string;
    count: number;
    last_occurrence: string;
    affected_brokers: number;
  }>;
};

export default function DashboardPage() {
  const q = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<Dashboard>("/api/v1/admin/dashboard"),
    refetchInterval: 5000,
  });

  useEffect(() => {
    if (!isSessionValid()) {
      redirectToLogin();
      return;
    }
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const host = import.meta.env.VITE_API_URL ? new URL(import.meta.env.VITE_API_URL).host : location.host;
    const ws = new WebSocket(`${proto}://${host}/api/v1/admin/ws?token=${getToken()}`);
    let closedByUs = false;
    ws.onmessage = () => q.refetch();
    ws.onclose = () => {
      if (!closedByUs && !isSessionValid()) redirectToLogin();
    };
    return () => {
      closedByUs = true;
      ws.close();
    };
  }, [q]);

  if (q.isLoading) return <div>Загрузка…</div>;
  if (q.error) return <div className="text-danger">{(q.error as Error).message}</div>;
  const d = q.data!;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Дашборд</h1>
        <p className="text-sm text-slate-500">Здоровье шлюза и поток ордеров в реальном времени</p>
      </div>
      <div className="grid gap-4 md:grid-cols-3">
        {Object.entries(d.system).map(([k, v]) => (
          <div key={k} className="card">
            <div className="text-xs uppercase tracking-wide text-slate-500">{systemKeyLabel(k)}</div>
            <div className={`mt-1 text-xl font-semibold status-${v}`}>{statusLabel(v)}</div>
          </div>
        ))}
      </div>
      <div className="grid gap-4 md:grid-cols-4">
        <Stat label="Ордеров сегодня" value={d.stats.total} />
        <Stat label="Исполнено" value={d.stats.filled} />
        <Stat label="Отклонено" value={d.stats.rejected} />
        <Stat label="Активных" value={d.stats.active} />
      </div>
      {d.observability && (
        <div className="grid gap-4 md:grid-cols-4 xl:grid-cols-8">
          <Stat label="Запросов сегодня" value={d.observability.requests_today} />
          <Stat label="Успешных" value={d.observability.successful} />
          <Stat label="Ошибок запросов" value={d.observability.failed} />
          <Stat label="Success rate" value={formatPct(d.observability.success_rate)} raw />
          <Stat label="Avg latency" value={formatMs(d.observability.avg_latency_ms)} raw />
          <Stat label="P95" value={formatMs(d.observability.p95_latency_ms)} raw />
          <Stat label="P99" value={formatMs(d.observability.p99_latency_ms)} raw />
          <Stat label="Auth failures" value={d.observability.auth_failures_today} />
        </div>
      )}
      <div className="grid gap-4 md:grid-cols-3">
        <Stat label="Ошибки сегодня" value={d.observability?.errors_today ?? 0} />
        <Stat label="Critical" value={d.observability?.critical_today ?? 0} />
        <Stat label="Подключены сейчас" value={d.observability?.connected_now ?? d.connected_brokers?.length ?? 0} />
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="card">
          <h2 className="mb-3 font-medium">FIX-адаптеры</h2>
          <div className="space-y-2">
            {d.adapters.filter((a) => !isHiddenAdapter(a)).map((a) => (
              <div key={a.id} className="flex items-center justify-between rounded-lg bg-[#f4f7f8] px-3 py-2">
                <div>
                  <div className="font-medium">{displayName(a.name)}</div>
                  <div className="font-mono text-xs text-slate-500">
                    {a.id} · вес {a.weight}
                  </div>
                </div>
                <div className="text-right">
                  <div className={`font-semibold status-${a.status}`}>{statusLabel(a.status)}</div>
                  <div className="text-xs text-slate-500">
                    активных {a.active_orders} · готово {a.processed_orders}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="card">
          <div className="mb-3 flex items-baseline justify-between gap-3">
            <h2 className="font-medium">Подключённые брокеры</h2>
            <div className="text-xs text-slate-500">
              {d.connected_brokers?.length || 0} из {d.brokers} · запросы за 2 мин
            </div>
          </div>
          <div className="space-y-2">
            {(d.connected_brokers || []).map((b) => (
              <Link
                key={b.broker_id}
                to={`/brokers/${b.broker_id}?tab=activity`}
                className="flex items-center justify-between rounded-lg bg-[#f4f7f8] px-3 py-2 hover:bg-slate-100"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="h-2 w-2 shrink-0 rounded-full bg-emerald-500" />
                    <span className="truncate font-medium">{b.broker_name || b.broker_id}</span>
                  </div>
                  <div className="mt-0.5 font-mono text-xs text-slate-500">
                    {b.broker_code || "—"}
                    {b.last_ip ? ` · ${b.last_ip}` : ""}
                  </div>
                </div>
                <div className="text-right text-xs text-slate-500">
                  <div>{formatRelative(b.last_activity)}</div>
                  <div>{formatNumber(b.requests)} запр.</div>
                </div>
              </Link>
            ))}
            {(d.connected_brokers || []).length === 0 && (
              <div className="py-6 text-center text-sm text-slate-400">Сейчас никто не подключён</div>
            )}
          </div>
        </div>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="card overflow-x-auto">
          <h2 className="mb-3 font-medium">Самые активные брокеры</h2>
          <table className="table">
            <thead>
              <tr>
                <th>Брокер</th>
                <th>Запросы</th>
                <th>Заявки</th>
                <th>Ошибки</th>
                <th>Success</th>
                <th>Avg</th>
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
                  <td colSpan={6} className="py-6 text-center text-slate-400">
                    Пока нет API-активности
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <div className="card overflow-x-auto">
          <h2 className="mb-3 font-medium">Топ ошибок</h2>
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
                    <Link className="font-mono text-xs hover:text-accent" to={`/logs/errors`}>
                      {e.error}
                    </Link>
                  </td>
                  <td>{formatNumber(e.count)}</td>
                  <td className="text-xs">{formatDateTime(e.last_occurrence)}</td>
                  <td>{e.affected_brokers}</td>
                </tr>
              ))}
              {(d.top_errors || []).length === 0 && (
                <tr>
                  <td colSpan={4} className="py-6 text-center text-slate-400">
                    Ошибок за сегодня нет
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function Stat({ label, value, raw }: { label: string; value: number | string; raw?: boolean }) {
  return (
    <div className="card">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 font-mono text-2xl font-semibold">{raw ? value : formatNumber(Number(value) || 0)}</div>
    </div>
  );
}
