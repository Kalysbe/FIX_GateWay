import type { ReactElement, ReactNode } from "react";
import { formatDateTime, formatMs, formatNumber, formatPct, formatTime } from "../../labels";
import { Icons } from "../../components/icons";
import { PageSpinner } from "../../components/feedback";
import type { AuditRow, ObsLog } from "../../types/admin";

export type BrokerAPIStats = {
  today: { requests: number; successful: number; errors: number; success_rate: number; avg_latency_ms: number; p95_latency_ms: number; p99_latency_ms: number };
  week: { requests: number; successful: number; errors: number; success_rate: number; avg_latency_ms: number; p95_latency_ms: number; p99_latency_ms: number };
  month: { requests: number; successful: number; errors: number; success_rate: number; avg_latency_ms: number; p95_latency_ms: number; p99_latency_ms: number };
  last_activity?: string;
  top_errors?: Array<{ error: string; count: number; last_occurrence: string }>;
  series?: Array<{ bucket: string; requests: number; successful: number; errors: number }>;
};

export const PERM_ICON: Record<string, (p: { size?: number; className?: string }) => ReactElement> = {
  "orders:create": Icons.cart,
  "orders:read": Icons.eye,
  "orders:cancel": Icons.x,
  "orders:replace": Icons.refresh,
  "trades:read": Icons.book,
  "positions:read": Icons.chart,
  "positions:write": Icons.edit,
};

export function SummaryChip({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-[110px] rounded-lg border border-line bg-panel px-3 py-2">
      <div className="text-[11px] text-slate-500">{label}</div>
      <div className="text-sm font-semibold">{value}</div>
    </div>
  );
}

export function DetailRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="text-slate-500">{label}</dt>
      <dd className="text-right font-medium text-ink">{value ?? "—"}</dd>
    </div>
  );
}

export function pctDelta(current?: number, previous?: number): { text: string; up: boolean | null } {
  const cur = Number(current ?? 0);
  const prev = Number(previous ?? 0);
  if (prev === 0 && cur === 0) return { text: "0%", up: null };
  if (prev === 0) return { text: "+100%", up: true };
  const pct = ((cur - prev) / prev) * 100;
  const rounded = Math.round(pct * 10) / 10;
  const sign = rounded > 0 ? "+" : "";
  return { text: `${sign}${rounded}%`, up: rounded === 0 ? null : rounded > 0 };
}

export function Metric({
  label,
  value,
  delta,
}: {
  label: string;
  value: string;
  delta: { text: string; up: boolean | null };
}) {
  const tone = delta.up === true ? "text-emerald-600" : delta.up === false ? "text-danger" : "text-slate-400";
  return (
    <div>
      <div className="text-[11px] text-slate-500">{label}</div>
      <div className="text-base font-semibold">{value}</div>
      <div className={`text-xs font-medium ${tone}`}>{delta.text} к пред. периоду</div>
    </div>
  );
}

export function RequestChart({ series }: { series: Array<{ date: string; orders: number }> }) {
  const points = series.length ? series.map((s) => Number(s.orders) || 0) : [0];
  const w = 320;
  const h = 120;
  const max = Math.max(...points, 1);
  const step = points.length > 1 ? w / (points.length - 1) : w;
  const coords = points.map((p, i) => `${i * step},${h - (p / max) * (h - 16) - 8}`).join(" ");
  const area = `0,${h} ${coords} ${points.length > 1 ? w : 0},${h}`;
  const empty = points.every((p) => p === 0);
  return (
    <div className="relative">
      <svg viewBox={`0 0 ${w} ${h}`} className="h-32 w-full" preserveAspectRatio="none">
        <defs>
          <linearGradient id="reqFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#57b6c0" stopOpacity="0.35" />
            <stop offset="100%" stopColor="#57b6c0" stopOpacity="0.02" />
          </linearGradient>
        </defs>
        <polygon points={area} fill="url(#reqFill)" />
        <polyline points={coords} fill="none" stroke="#57b6c0" strokeWidth="2.5" strokeLinejoin="round" />
      </svg>
      {empty && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-slate-400">
          Нет заявок за период
        </div>
      )}
    </div>
  );
}

function activityMeta(action: string, result?: string | null, error?: string | null) {
  const a = (action || "").toUpperCase();
  const failed = !!(error || (result && /fail|error|deny|reject/i.test(result)));
  if (failed || a.includes("FAIL") || a.includes("AUTH_FAIL")) {
    return { label: "Ошибка аутентификации", tone: "bg-rose-50 text-danger", Icon: Icons.x };
  }
  if (a.includes("ORDER") || a.includes("CREATE")) {
    return { label: "Заявка создана", tone: "bg-sky-50 text-sky-600", Icon: Icons.plus };
  }
  if (a.includes("TRADE") || a.includes("FILL") || a.includes("EXEC")) {
    return { label: "Сделка получена", tone: "bg-sky-50 text-sky-600", Icon: Icons.trades };
  }
  if (a.includes("AUTH") || a.includes("LOGIN") || a.includes("TOKEN")) {
    return { label: "Успешная аутентификация", tone: "bg-emerald-50 text-emerald-600", Icon: Icons.check };
  }
  return { label: action || "Событие", tone: "bg-slate-100 text-slate-600", Icon: Icons.info };
}

export function ActivityList({ items, empty }: { items: AuditRow[]; empty: string }) {
  if (!items.length) return <p className="text-sm text-slate-400">{empty}</p>;
  return (
    <ul className="space-y-3">
      {items.map((item) => {
        const meta = activityMeta(item.action, item.result, item.error);
        return (
          <li key={item.id} className="flex gap-2.5">
            <span className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full ${meta.tone}`}>
              <meta.Icon size={14} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">{meta.label}</div>
              <div className="text-[11px] text-slate-400">
                {formatTime(item.created_at)}
                {item.ip ? ` · ${item.ip}` : ""}
              </div>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

export function BrokerActivity({
  st,
  range,
  onRange,
  errors,
  loading,
}: {
  st?: BrokerAPIStats;
  range: string;
  onRange: (v: string) => void;
  errors: ObsLog[];
  loading: boolean;
}) {
  if (loading && !st) return <PageSpinner />;
  const today = st?.today;
  const week = st?.week;
  const month = st?.month;
  return (
    <div className="space-y-4">
      <section className="card space-y-4">
        <h2 className="font-semibold">Активность</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <SummaryChip label="Сегодня" value={formatNumber(today?.requests)} />
          <SummaryChip label="7 дней" value={formatNumber(week?.requests)} />
          <SummaryChip label="Месяц" value={formatNumber(month?.requests)} />
        </div>
        <div className="grid gap-3 sm:grid-cols-5">
          <SummaryChip label="Доля успеха" value={formatPct(today?.success_rate)} />
          <SummaryChip label="Ошибки" value={formatNumber(today?.errors)} />
          <SummaryChip label="Среднее" value={formatMs(today?.avg_latency_ms)} />
          <SummaryChip label="P95" value={formatMs(today?.p95_latency_ms)} />
          <SummaryChip label="P99" value={formatMs(today?.p99_latency_ms)} />
        </div>
      </section>
      <section className="card space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">Запросы</h2>
          <select className="input w-auto" value={range} onChange={(e) => onRange(e.target.value)}>
            <option value="24h">24 часа</option>
            <option value="7d">7 дней</option>
            <option value="30d">30 дней</option>
          </select>
        </div>
        <MiniSeries series={st?.series || []} />
      </section>
      <section className="card space-y-3">
        <h2 className="font-semibold">Ошибки</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <SummaryChip label="Сегодня" value={formatNumber(today?.errors)} />
          <SummaryChip label="Неделя" value={formatNumber(week?.errors)} />
          <SummaryChip label="Месяц" value={formatNumber(month?.errors)} />
        </div>
        <div className="space-y-1 text-sm">
          {(st?.top_errors || []).map((e) => (
            <div key={e.error} className="flex justify-between rounded-lg bg-slate-50 px-3 py-2">
              <span className="font-mono text-xs">{e.error}</span>
              <span>{e.count}</span>
            </div>
          ))}
        </div>
        {errors.length === 0 ? (
          <p className="text-sm text-slate-400">Ошибок за выбранный период не обнаружено.</p>
        ) : (
          <ul className="space-y-2">
            {errors.map((e) => (
              <li key={e.id} className="rounded-lg border border-rose-100 bg-rose-50 px-3 py-2 text-sm">
                <div className="font-medium text-danger">
                  {e.severity} · {e.error_code || e.subtype}
                </div>
                <div className="text-xs text-slate-500">{formatDateTime(e.timestamp)}</div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function MiniSeries({ series }: { series: Array<{ bucket: string; requests: number; successful: number; errors: number }> }) {
  const points = series.length ? series.map((s) => Number(s.requests) || 0) : [0];
  const err = series.length ? series.map((s) => Number(s.errors) || 0) : [0];
  const w = 480;
  const h = 120;
  const max = Math.max(...points, ...err, 1);
  const step = points.length > 1 ? w / (points.length - 1) : w;
  const line = (arr: number[]) => arr.map((p, i) => `${i * step},${h - (p / max) * (h - 16) - 8}`).join(" ");
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="h-32 w-full" preserveAspectRatio="none">
      <polyline points={line(points)} fill="none" stroke="#57b6c0" strokeWidth="2" />
      <polyline points={line(err)} fill="none" stroke="#d6455d" strokeWidth="2" />
    </svg>
  );
}
