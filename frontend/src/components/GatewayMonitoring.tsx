import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import type { Dashboard } from "../types/admin";
import { displayAdapterName, isHiddenAdapter, isSimulator } from "../lib/adapters";
import { StatusBadge } from "./StatusBadge";
import { ErrorBanner } from "./feedback";

type Sample = {
  timestamp: string;
  counters: Record<string, number>;
  adapters: Record<string, Record<string, number>>;
  heap_bytes: number;
  goroutines: number;
  cpu_cores: number;
  database: { acquired: number; total: number; max: number } | null;
};
const number = (n: number) => n.toLocaleString("ru-RU", { maximumFractionDigits: 1 });

export function GatewayMonitoring({ dashboard: d }: { dashboard?: Dashboard }) {
  const operationalAdapters = (d?.adapters ?? []).filter(a => !isHiddenAdapter(a));
  const simulatorOnly = operationalAdapters.length > 0 && operationalAdapters.every(isSimulator);

  const query = useQuery({
    queryKey: ["monitoring"],
    queryFn: ({ signal }) => api<Sample>("/api/v1/admin/monitoring", { signal }),
    refetchInterval: 5000,
  });
  const historyQuery = useQuery({
    queryKey: ["monitoring-history"],
    queryFn: ({ signal }) => api<{ series: Record<string, { time: string; value: number | null }[]>; adapters: Record<string, { time: string; value: number | null }[]> }>("/api/v1/admin/monitoring/history", { signal }),
    refetchInterval: 15000,
    staleTime: 15000,
    retry: false,
  });
  const latest = query.data;
  const points = (key: string, adapter?: string) => adapter
    ? historyQuery.data?.adapters[adapter] ?? []
    : historyQuery.data?.series[key] ?? [];
  const cpu = points("cpu_pct").at(-1)?.value;
  return <section className="space-y-4">
    <div className="card">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[15px] font-semibold">Путь заявки и ответа</h2>
        <Link className="text-sm text-accent" to="/orders">Открыть заявки и трассировку →</Link>
      </div>
      <div className="mt-5 overflow-x-auto pb-2">
        <div className="grid min-w-[720px] grid-cols-[1fr_70px_1fr_70px_1fr_70px_1fr] items-center gap-2">
          <Node title="Брокеры" detail={d ? `${d.observability?.connected_now ?? 0} активны за 2 мин` : "Ожидание статистики"} />
          <Arrows forward="HTTP / gRPC" back="Ответ / события" />
          <Node title="Наш шлюз" detail={`Очередь: ${latest ? number(latest.counters.orders_queue_size ?? 0) : "—"}`} />
          <Arrows forward="Маршрутизация" back="Статусы / сделки" />
          <Node title="FIX-адаптеры" detail={d ? `${d.adapters_available} доступны` : "Ожидание статистики"} />
          <Arrows forward="FIX-заявка" back="FIX-отчёт" />
          <Node title={simulatorOnly ? "FIX-симулятор" : "Биржа / QUIK"} detail={simulatorOnly ? "Тестовый контур · без реальной биржи" : "Связь через FIX-сессию"} />
        </div>
      </div>
      <p className="mt-3 text-xs text-muted">Схема маршрута. Фактические отправки и ответы доступны в карточке заявки. Приём API-запроса и исполнение на бирже — отдельные события.</p>
    </div>
    {query.error && <ErrorBanner error={query.error} onRetry={() => query.refetch()} />}
    {historyQuery.error && <ErrorBanner error={historyQuery.error} onRetry={() => historyQuery.refetch()} />}
    <div className="card space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[15px] font-semibold">Нагрузка нашего сервера</h2>
        <span className="text-xs text-muted">{latest ? `Замер: ${new Date(latest.timestamp).toLocaleTimeString("ru-RU")}` : "Ожидание замера"} · каждые 5 с</span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Metric label="CPU процесса" value={cpu == null ? "—" : `${number(cpu)}%`} detail="100% = одно ядро" />
        <Metric label="Память процесса (RSS)" value={latest?.counters.process_resident_memory_bytes == null ? "—" : `${number(latest.counters.process_resident_memory_bytes / 1048576)} МБ`} detail={`Go heap: ${latest ? number(latest.heap_bytes / 1048576) : "—"} МБ`} />
        <Metric label="Соединения БД заняты" value={latest?.database ? `${latest.database.acquired} / ${latest.database.max}` : "—"} detail={`Всего открыто: ${latest?.database?.total ?? "—"}`} />
        <Metric label="Горутины" value={latest ? number(latest.goroutines) : "—"} detail={`CPU-ядер: ${latest?.cpu_cores ?? "—"}`} />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Chart title="CPU процесса, % одного ядра" data={points("cpu_pct")} color="#7c3aed" />
        <Chart title="Память процесса (RSS), МБ" data={points("rss_mb")} color="#0891b2" />
        <Chart title="HTTP-запросы / с" data={points("http_rps")} color="#0284c7" />
        <Chart title="Очередь заявок" data={points("queue")} color="#d97706" />
      </div>
      <p className="text-xs text-muted">История за последние 10 минут хранится в Prometheus. Скорости рассчитываются за минуту; графики обновляются каждые 15 с. HTTP включает административные запросы. CPU и RSS относятся к процессу шлюза; недоступные замеры обозначены «—».</p>
    </div>
    <div className="grid gap-4 lg:grid-cols-2">
      {operationalAdapters.map(a => {
        const counters = latest?.adapters[a.id];
        return <div key={a.id} className="card space-y-3">
          <div className="flex items-center justify-between gap-2"><Link to="/adapters" className="font-semibold hover:text-accent">{displayAdapterName(a.name)} <span className="font-mono text-xs text-muted">{a.id}</span></Link><StatusBadge status={a.status} /></div>
          <div className="grid grid-cols-3 gap-2">
            <Metric label="Активные заявки" value={number(a.active_orders ?? counters?.adapter_active_orders ?? 0)} />
            <Metric label="Попытки отправки" value={counters?.adapter_requests_total == null ? "—" : number(counters.adapter_requests_total)} />
            <Metric label="Ошибки отправки" value={counters?.adapter_errors_total == null ? "—" : number(counters.adapter_errors_total)} />
          </div>
          <Chart title="Отправки в адаптер / с" data={points("adapter_rps", a.id)} color="#059669" />
          <p className="text-xs text-muted">Счётчики отправок с запуска шлюза. CPU и память удалённого адаптера не передаются.</p>
        </div>;
      })}
      {d && operationalAdapters.length === 0 && <div className="card text-muted">Нет включённых адаптеров.</div>}
    </div>
  </section>;
}

function Node({ title, detail }: { title: string; detail: string }) {
  return <div className="rounded-lg border border-slate-200 bg-surface p-4 text-center"><div className="font-semibold">{title}</div><div className="mt-2 text-xs text-muted">{detail}</div></div>;
}
function Arrows({ forward, back }: { forward: string; back: string }) {
  return <div className="text-center"><div className="text-[10px] text-muted">{forward}</div><div className="text-2xl text-sky-600">→</div><div className="text-2xl text-emerald-600">←</div><div className="text-[10px] text-muted">{back}</div></div>;
}
function Metric({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return <div className="rounded-lg bg-surface p-3"><div className="text-xs text-muted">{label}</div><div className="mt-1 font-mono text-lg">{value}</div>{detail && <div className="mt-1 text-xs text-muted">{detail}</div>}</div>;
}
function Chart({ title, data, color }: { title: string; data: { time: string; value: number | null }[]; color: string }) {
  const valid = data.filter(p => p.value != null);
  const max = Math.max(1, ...valid.map(p => p.value!));
  const start = data.length ? Date.parse(data[0].time) : 0;
  const end = data.length ? Date.parse(data.at(-1)!.time) : 0;
  const x = (time: string) => 45 + (Date.parse(time) - start) / Math.max(1, end - start) * 535;
  const y = (v: number) => 135 - v / max * 110;
  let drawing = false;
  const path = data.map(p => {
    if (p.value == null) { drawing = false; return ""; }
    const command = `${drawing ? "L" : "M"}${x(p.time)},${y(p.value)}`;
    drawing = true;
    return command;
  }).join(" ");
  return <div className="rounded-lg border border-slate-200 p-3">
    <div className="flex justify-between text-sm"><span>{title}</span><span className="font-mono">{data.at(-1)?.value == null ? "—" : number(data.at(-1)!.value!)}</span></div>
    {valid.length < 2 ? <div className="flex h-40 items-center justify-center text-xs text-muted">История метрик пока недоступна</div> : <>
      <svg viewBox="0 0 600 155" className="mt-2 w-full" role="img" aria-label={title}>
        {[0, 0.5, 1].map(v => <g key={v}><line x1="45" x2="580" y1={y(v * max)} y2={y(v * max)} stroke="#e2e8f0" /><text x="2" y={y(v * max) + 4} fontSize="10" fill="#64748b">{number(v * max)}</text></g>)}
        <path d={path} fill="none" stroke={color} strokeWidth="2" />
        {valid.map(p => <circle key={p.time} cx={x(p.time)} cy={y(p.value!)} r="3" fill={color}><title>{new Date(p.time).toLocaleTimeString("ru-RU")} · {number(p.value!)}</title></circle>)}
      </svg>
      <div className="flex justify-between text-[10px] text-muted"><span>{new Date(start).toLocaleTimeString("ru-RU")}</span><span>{new Date(end).toLocaleTimeString("ru-RU")}</span></div>
    </>}
  </div>;
}
