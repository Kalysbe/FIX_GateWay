import type { ReactElement, ReactNode } from "react";
import { formatDateTime, formatMs, formatNumber, formatPct, formatTime } from "../../labels";
import { Icons } from "../../components/icons";
import { PageSpinner } from "../../components/feedback";
import { CategoryChart, CHART_COLORS, CHART_DANGER } from "../../components/charts";
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
};

export function SummaryChip({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-[110px] rounded-lg border border-line bg-panel px-3 py-2">
      <div className="text-[11px] text-muted">{label}</div>
      <div className="text-sm font-semibold">{value}</div>
    </div>
  );
}

export function DetailRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="text-muted">{label}</dt>
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
  const tone = delta.up === true ? "text-success" : delta.up === false ? "text-danger" : "text-faint";
  return (
    <div>
      <div className="text-[11px] text-muted">{label}</div>
      <div className="text-base font-semibold">{value}</div>
      <div className={`text-xs font-medium ${tone}`}>{delta.text} к пред. периоду</div>
    </div>
  );
}

// Buckets come as dates or timestamps; the axis shows them short.
function shortBucket(value: string) {
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return `${value.slice(8, 10)}.${value.slice(5, 7)}`;
  const t = Date.parse(value);
  return Number.isFinite(t) ? new Date(t).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }) : value;
}

export function RequestChart({ series }: { series: Array<{ date: string; orders: number }> }) {
  const data = series.map(s => ({ date: s.date, orders: Number(s.orders) || 0 }));
  const empty = data.every(p => p.orders === 0);
  return (
    <div className="relative">
      <CategoryChart label="Заявки за период" data={data.length ? data : [{ date: "", orders: 0 }]} xKey="date" formatX={shortBucket} format={formatNumber} series={[{ key: "orders", label: "Заявки", color: CHART_COLORS[0], area: true }]} />
      {empty && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-faint">
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
    return { label: "Ошибка аутентификации", tone: "bg-danger-soft text-danger", Icon: Icons.x };
  }
  if (a.includes("ORDER") || a.includes("CREATE")) {
    return { label: "Заявка создана", tone: "bg-info-soft text-info", Icon: Icons.plus };
  }
  if (a.includes("TRADE") || a.includes("FILL") || a.includes("EXEC")) {
    return { label: "Сделка получена", tone: "bg-info-soft text-info", Icon: Icons.trades };
  }
  if (a.includes("AUTH") || a.includes("LOGIN") || a.includes("TOKEN")) {
    return { label: "Успешная аутентификация", tone: "bg-success-soft text-success", Icon: Icons.check };
  }
  return { label: action || "Событие", tone: "bg-subtle text-muted", Icon: Icons.info };
}

export function ActivityList({ items, empty }: { items: AuditRow[]; empty: string }) {
  if (!items.length) return <p className="text-sm text-faint">{empty}</p>;
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
              <div className="text-[11px] text-faint">
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
            <div key={e.error} className="flex justify-between rounded-lg bg-subtle px-3 py-2">
              <span className="font-mono text-xs">{e.error}</span>
              <span>{e.count}</span>
            </div>
          ))}
        </div>
        {errors.length === 0 ? (
          <p className="text-sm text-faint">Ошибок за выбранный период не обнаружено.</p>
        ) : (
          <ul className="space-y-2">
            {errors.map((e) => (
              <li key={e.id} className="rounded-lg border border-danger/20 bg-danger-soft px-3 py-2 text-sm">
                <div className="font-medium text-danger">
                  {e.severity} · {e.error_code || e.subtype}
                </div>
                <div className="text-xs text-muted">{formatDateTime(e.timestamp)}</div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function MiniSeries({ series }: { series: Array<{ bucket: string; requests: number; successful: number; errors: number }> }) {
  const data = series.map(s => ({ bucket: s.bucket, requests: Number(s.requests) || 0, errors: Number(s.errors) || 0 }));
  if (!data.length) return <div className="flex h-32 items-center justify-center text-xs text-faint">Нет запросов за период</div>;
  return (
    <>
      <CategoryChart label="Запросы и ошибки за период" data={data} xKey="bucket" formatX={shortBucket} format={formatNumber} series={[{ key: "requests", label: "Запросы", color: CHART_COLORS[0], area: true }, { key: "errors", label: "Ошибки", color: CHART_DANGER }]} />
      <div className="flex gap-4 text-[11px] text-muted"><span className="flex items-center gap-1.5"><i className="h-[3px] w-2 rounded" style={{ background: CHART_COLORS[0] }} />Запросы</span><span className="flex items-center gap-1.5"><i className="h-[3px] w-2 rounded" style={{ background: CHART_DANGER }} />Ошибки</span></div>
    </>
  );
}