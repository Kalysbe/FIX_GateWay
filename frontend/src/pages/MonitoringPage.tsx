import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import type { Adapter, Dashboard } from "../types/admin";
import { StatusBadge } from "../components/StatusBadge";
import { useBreadcrumbs } from "../components/breadcrumbs";
import { ErrorBanner, PageSpinner } from "../components/feedback";
import { displayAdapterName, isHiddenAdapter } from "../lib/adapters";
import "./monitoring.css";

type Sample = { timestamp: string; counters: Record<string, number>; adapters: Record<string, Record<string, number>>; heap_bytes: number; goroutines: number; cpu_cores: number; database: { acquired: number; total: number; max: number } | null };
type Point = { time: string; value: number | null };
const colors = ["#57b6c0", "#1f8a78", "#c47a00", "#8393ad"];
const num = (v: number | null | undefined, suffix = "") => v == null || !Number.isFinite(v) ? "—" : `${v.toLocaleString("ru-RU", { maximumFractionDigits: 1 })}${suffix}`;
function rate(s: Sample, prev: Sample | undefined, key: string) {
  if (!prev) return null;
  const dt = (Date.parse(s.timestamp) - Date.parse(prev.timestamp)) / 1000;
  const a = s.counters[key], b = prev.counters[key];
  return dt > 0 && dt <= 20 && a != null && b != null && a >= b ? (a - b) / dt : null;
}
function Panel({ title, note, children, className = "" }: { title: string; note?: string; children: ReactNode; className?: string }) {
  return <section className={`monitor-panel ${className}`}><header><h2>{title}</h2>{note && <span>{note}</span>}</header>{children}</section>;
}
function Chart({ series, compact = false }: { series: { label: string; points: Point[]; color: string }[]; compact?: boolean }) {
  const points = series.flatMap(s => s.points), valid = points.filter(p => p.value != null);
  if (valid.length < 2 || !series.some(s => s.points.filter(p => p.value != null).length >= 2)) return <div className={compact ? "monitor-spark-empty" : "monitor-chart-empty"}>{compact ? "Ожидание истории" : "Накапливаем замеры…"}</div>;
  const max = Math.max(1, ...valid.map(p => p.value!)), start = Math.min(...points.map(p => Date.parse(p.time))), end = Math.max(...points.map(p => Date.parse(p.time)));
  const x = (t: string) => 42 + (Date.parse(t) - start) / Math.max(1, end - start) * 508;
  const y = (v: number) => 148 - v / max * 125;
  return <div className={compact ? "monitor-spark" : "monitor-chart"}><svg viewBox={compact ? "40 15 515 138" : "0 0 565 177"} role="img" aria-label={series.map(s => s.label).join(", ")}>
    {!compact && [0, .25, .5, .75, 1].map(f => <g key={f}><line x1="42" x2="550" y1={y(max*f)} y2={y(max*f)} stroke="#e8eef1"/><text x="0" y={y(max*f)+4} fontSize="10" fill="#64748b">{num(max*f)}</text></g>)}
    {series.map(s => { let drawing = false; const path = s.points.map(p => { if (p.value == null) { drawing = false; return ""; } const part = `${drawing ? "L" : "M"}${x(p.time)},${y(p.value)}`; drawing = true; return part; }).join(" "); return <g key={s.label}><path d={path} fill="none" stroke={s.color} strokeWidth="2" vectorEffect="non-scaling-stroke"/>{!compact && s.points.filter(p => p.value != null).map(p => <circle key={p.time} cx={x(p.time)} cy={y(p.value!)} r="2" fill={s.color}><title>{new Date(p.time).toLocaleTimeString("ru-RU")} · {num(p.value)}</title></circle>)}</g>; })}
    {!compact && <><text x="42" y="172" fontSize="10" fill="#64748b">{new Date(start).toLocaleTimeString("ru-RU")}</text><text x="550" y="172" textAnchor="end" fontSize="10" fill="#64748b">{new Date(end).toLocaleTimeString("ru-RU")}</text></>}
  </svg>{!compact && <div className="monitor-legend">{series.map(s => <span key={s.label}><i style={{ background: s.color }}/>{s.label}</span>)}</div>}</div>;
}
function Gauge({ label, value, detail, color }: { label: string; value: number | null; detail: string; color: string }) {
  return <div className="monitor-gauge"><span>{label}</span><div><svg viewBox="0 0 110 110" aria-hidden="true"><circle cx="55" cy="55" r="44" fill="none" stroke="#edf2f4" strokeWidth="9"/><circle cx="55" cy="55" r="44" fill="none" stroke={color} strokeWidth="9" strokeLinecap="round" strokeDasharray={`${Math.max(0, Math.min(100, value ?? 0))*2.765} 276.5`} transform="rotate(-90 55 55)"/></svg><strong>{num(value, "%")}</strong></div><small>{detail}</small></div>;
}
export default function MonitoringPage() {
  useBreadcrumbs([{ label: "Мониторинг" }]);
  const [history, setHistory] = useState<Sample[]>([]);
  const [minutes, setMinutes] = useState(10);
  const [paused, setPaused] = useState(false);
  const [dailyEnabled, setDailyEnabled] = useState(false);
  const boundedSignal = (signal: AbortSignal) => AbortSignal.any([signal, AbortSignal.timeout(5000)]);
  // Day-wide log aggregates are optional and never gate live monitoring.
  const dashboard = useQuery({ queryKey: ["monitoring-daily"], queryFn: ({ signal }) => api<Dashboard>("/api/v1/admin/dashboard", { signal: boundedSignal(signal) }), enabled: dailyEnabled, staleTime: 60000, retry: false });
  const adapters = useQuery({ queryKey: ["monitoring-adapters"], queryFn: ({ signal }) => api<{ adapters: Adapter[] }>("/api/v1/admin/adapters", { signal: boundedSignal(signal) }), refetchInterval: paused ? false : 5000, retry: false });
  const monitoring = useQuery({ queryKey: ["monitoring"], queryFn: ({ signal }) => api<Sample>("/api/v1/admin/monitoring", { signal: boundedSignal(signal) }), refetchInterval: paused ? false : 5000, retry: false });
  useEffect(() => { if (!monitoring.data) return; const s = monitoring.data; setHistory(old => old.at(-1)?.timestamp === s.timestamp ? old : [...old, s].filter(p => Date.parse(p.timestamp) >= Date.parse(s.timestamp)-1800000).slice(-361)); }, [monitoring.data]);
  if (monitoring.isLoading) return <PageSpinner/>;
  if (!monitoring.data) return <ErrorBanner error={monitoring.error} onRetry={() => monitoring.refetch()}/>;
  const latest = history.at(-1) ?? monitoring.data, prev = history.at(-2);
  const runtimeAdapters = adapters.data?.adapters ?? dashboard.data?.adapters ?? [];
  const d = {
    adapters: runtimeAdapters,
    adapters_available: runtimeAdapters.filter(a => a.enabled !== false && ["ONLINE", "DEGRADED"].includes(a.status)).length,
    observability: dashboard.data?.observability,
    connected_brokers: dashboard.data?.connected_brokers,
    stats: dashboard.data?.stats ?? { active: undefined },
    top_errors: dashboard.data?.top_errors,
    system: dashboard.data?.system ?? { api: monitoring.error ? "UNKNOWN" : "ONLINE", database: "UNKNOWN", queue: "UNKNOWN" },
  };
  const obs = d.observability;
  const windowStart = latest ? Date.parse(latest.timestamp)-minutes*60000 : 0;
  const series = (label: string, key: string, color: string, multiplier = 1) => ({ label, color, points: history.map((s, i) => ({ time: s.timestamp, value: rate(s, history[i-1], key) == null ? null : rate(s, history[i-1], key)!*multiplier })).filter(p => Date.parse(p.time)>=windowStart) });
  const gauges = (label: string, get: (s: Sample) => number | null, color: string) => ({ label, color, points: history.filter(s => Date.parse(s.timestamp)>=windowStart).map(s => ({ time: s.timestamp, value: get(s) })) });
  const cpu = latest ? rate(latest, prev, "process_cpu_seconds_total") : null;
  const cpuPercent = cpu == null || !latest?.cpu_cores ? null : cpu*100/latest.cpu_cores;
  const db = latest?.database;
  const errors = obs && obs.requests_today > 0 ? obs.failed/obs.requests_today*100 : null;
  const cards = [
    { label: "HTTP-запросы", value: num(latest ? rate(latest, prev, "gateway_http_requests_total") : null, " /с"), note: "Включая административные", chart: series("HTTP", "gateway_http_requests_total", colors[0]) },
    { label: "Подключённые брокеры", value: num(obs?.connected_now ?? d.connected_brokers?.length), note: "Активность за последние 2 мин", chart: null },
    { label: "Задержка · p95", value: num(obs?.p95_latency_ms, " мс"), note: "Запросы за сегодня", chart: null },
    { label: "Доля ошибок", value: num(errors, "%"), note: "Запросы за сегодня", chart: null },
    { label: "Доступные адаптеры", value: `${d.adapters_available} / ${d.adapters.filter(a => !isHiddenAdapter(a)).length}`, note: "FIX-соединения", chart: null },
  ];
  return <div className="monitoring-page">
    <div className="monitor-heading"><div><h1>Мониторинг</h1><p>Состояние шлюза в реальном времени</p></div><div className="monitor-controls"><span className="monitor-live"><i style={{ background: paused ? colors[2] : monitoring.error ? "#d6455d" : colors[1] }}/>{paused ? "Пауза" : monitoring.error ? "Нет связи" : "Обновление · 5 с"}</span><select aria-label="Период графиков" value={minutes} onChange={e => setMinutes(Number(e.target.value))}><option value={5}>5 минут</option><option value={10}>10 минут</option><option value={30}>30 минут</option></select><button className="btn-muted" onClick={() => setPaused(p => !p)}>{paused ? "Продолжить" : "Пауза"}</button></div></div>
    {dashboard.error && <ErrorBanner error={dashboard.error} onRetry={() => dashboard.refetch()}/>}
    {adapters.error && <ErrorBanner error={adapters.error} onRetry={() => adapters.refetch()}/>}
    {monitoring.error && <ErrorBanner error={monitoring.error} onRetry={() => monitoring.refetch()}/>}
    <div className="monitor-controls"><button className="btn-muted" disabled={dashboard.isFetching} onClick={() => { if (dailyEnabled) void dashboard.refetch(); else setDailyEnabled(true); }}>{dashboard.isFetching ? "Загрузка суточной статистики…" : "Загрузить суточную статистику"}</button><span>Суточные итоги загружаются отдельно от текущих показателей</span></div>
    <div className="monitor-kpis">{cards.map(c => <section className="monitor-panel monitor-kpi" key={c.label}><span>{c.label}</span><strong>{c.value}</strong><small>{c.note}</small></section>)}</div>
    <div className="monitor-grid">
      <Panel title="Поток запросов" note="в секунду" className="monitor-wide"><Chart series={[series("HTTP", "gateway_http_requests_total", colors[0]), series("Новые заявки", "orders_received_total", colors[1]), series("Обработано", "orders_processed_total", colors[2]), series("Отклонено", "orders_rejected_total", "#d6455d")]}/></Panel>
      <Panel title="Нагрузка шлюза"><div className="monitor-gauges"><Gauge label="CPU" value={cpuPercent} detail="От всех ядер" color={colors[0]}/><Gauge label="Пул БД" value={db && db.max > 0 ? db.acquired/db.max*100 : null} detail="Занятые соединения" color={colors[1]}/></div><div className="monitor-foot">Память RSS <b>{num(latest?.counters.process_resident_memory_bytes == null ? null : latest.counters.process_resident_memory_bytes/1048576, " МБ")}</b><span>Горутины <b>{num(latest?.goroutines)}</b></span></div></Panel>
      <Panel title="FIX-адаптеры" note="Текущее состояние"><div className="monitor-table-wrap"><table><thead><tr><th>Сервис</th><th>Активные</th><th>Статус</th></tr></thead><tbody>{d.adapters.filter(a => !isHiddenAdapter(a)).map(a => <tr key={a.id}><td><Link to="/adapters">{displayAdapterName(a.name)}</Link></td><td>{num(a.active_orders)}</td><td><StatusBadge status={a.status}/></td></tr>)}</tbody></table>{!d.adapters.filter(a => !isHiddenAdapter(a)).length && <p className="monitor-empty">Адаптеры не настроены</p>}</div></Panel>
      <Panel title="База данных" className="monitor-resource"><div className="monitor-inline-stats"><div><small>Занятые / лимит</small><strong>{db ? `${db.acquired} / ${db.max}` : "—"}</strong></div><div><small>Открыто соединений</small><strong>{num(db?.total)}</strong></div></div><Chart series={[gauges("Занятые", s => s.database?.acquired ?? null, colors[0]), gauges("Открытые", s => s.database?.total ?? null, colors[1])]}/></Panel>
      <Panel title="Память процесса" note="МБ" className="monitor-resource"><Chart series={[gauges("RSS", s => s.counters.process_resident_memory_bytes == null ? null : s.counters.process_resident_memory_bytes/1048576, colors[0]), gauges("Go heap", s => s.heap_bytes/1048576, colors[1])]}/></Panel>
      <Panel title="Очередь заявок" className="monitor-resource"><div className="monitor-inline-stats"><div><small>В очереди сейчас</small><strong>{num(latest?.counters.orders_queue_size)}</strong></div><div><small>Активных заявок</small><strong>{num(d.stats.active)}</strong></div></div><Chart series={[gauges("Очередь", s => s.counters.orders_queue_size ?? null, colors[2])]}/></Panel>
      <Panel title="Последние ошибки" note="За сегодня" className="monitor-wide"><div className="monitor-table-wrap"><table><thead><tr><th>Ошибка</th><th>Количество</th><th>Последнее событие</th></tr></thead><tbody>{(d.top_errors ?? []).slice(0,5).map(e => <tr key={e.error}><td><Link to={`/logs/errors?error_code=${encodeURIComponent(e.error)}`}>{e.error}</Link></td><td>{num(e.count)}</td><td>{new Date(e.last_occurrence).toLocaleString("ru-RU")}</td></tr>)}</tbody></table>{!d.top_errors?.length && <p className="monitor-empty">{dashboard.data ? "Ошибок за сегодня нет" : "Суточная статистика ещё не загружена"}</p>}</div><Link className="monitor-text-link" to="/logs">Все события →</Link></Panel>
      <Panel title="Задержка запросов" note="За сегодня" className="monitor-summary"><div className="monitor-latencies">{[["Средняя", obs?.avg_latency_ms], ["p95", obs?.p95_latency_ms], ["p99", obs?.p99_latency_ms]].map(([label, value]) => <div key={label}><span>{label}</span><strong>{num(value as number | undefined, " мс")}</strong></div>)}</div></Panel>
      <Panel title="Здоровье системы" className="monitor-summary">{Object.entries(d.system).map(([key, status]) => <div className="monitor-health" key={key}><span>{({ api: "API шлюза", database: "PostgreSQL", queue: "Очередь заявок" } as Record<string,string>)[key] ?? key}</span><StatusBadge status={status}/></div>)}</Panel>
    </div>
    <footer className="monitor-page-footer"><span>История накапливается, пока открыта страница · HTTP и ресурсы процесса шлюза</span><span>{latest ? `Последний замер: ${new Date(latest.timestamp).toLocaleTimeString("ru-RU")}` : "Ожидание телеметрии"}</span></footer>
  </div>;
}
