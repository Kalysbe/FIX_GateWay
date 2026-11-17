import { useId, useMemo } from "react";
import { Area, AreaChart, CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

/** Chart colours are theme tokens, so both themes draw them without overrides. */
export const CHART_COLORS = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"] as const;
export const CHART_DANGER = "var(--chart-danger)";

export type TimePoint = { time: string; value: number | null };
export type TimeSeries = { label: string; color: string; points: TimePoint[] };

const axis = { stroke: "var(--chart-grid)", tick: { fill: "var(--chart-text)", fontSize: 10 }, tickLine: false } as const;
const tooltipStyle = {
  contentStyle: { background: "var(--popover)", border: "1px solid var(--border)", borderRadius: 8, boxShadow: "var(--shadow-pop)", fontSize: 12, color: "var(--popover-foreground)" },
  labelStyle: { color: "var(--muted-foreground)", marginBottom: 2 },
  itemStyle: { padding: 0 },
  cursor: { stroke: "var(--chart-grid)" },
} as const;
const defaultFormat = (v: number) => v.toLocaleString("ru-RU", { maximumFractionDigits: 1 });
const clock = (ms: number) => new Date(ms).toLocaleTimeString("ru-RU");

/**
 * Lines over time. A missing value breaks its line rather than joining across
 * the gap. `compact` draws a sparkline without axes or grid.
 */
export function TimeSeriesChart({ series, height = 160, compact = false, label, format = defaultFormat, dots = false }: {
  series: TimeSeries[];
  height?: number;
  compact?: boolean;
  label?: string;
  format?: (value: number) => string;
  dots?: boolean;
}) {
  // One row per moment; each series is its own column.
  const data = useMemo(() => {
    const rows = new Map<number, Record<string, number | null>>();
    series.forEach((s, i) => s.points.forEach(p => {
      const t = Date.parse(p.time);
      if (!Number.isFinite(t)) return;
      const row = rows.get(t) ?? { t };
      row[`s${i}`] = p.value;
      rows.set(t, row);
    }));
    return [...rows.values()].sort((a, b) => (a.t ?? 0) - (b.t ?? 0));
  }, [series]);
  return <div role="img" aria-label={label ?? series.map(s => s.label).join(", ")} style={{ height }}>
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={data} margin={compact ? { top: 4, right: 2, bottom: 4, left: 2 } : { top: 8, right: 12, bottom: 0, left: 0 }}>
        {!compact && <CartesianGrid stroke="var(--chart-grid)" vertical={false} />}
        <XAxis dataKey="t" type="number" scale="time" domain={["dataMin", "dataMax"]} hide={compact} tickFormatter={clock} minTickGap={48} {...axis} />
        <YAxis hide={compact} width={44} tickFormatter={format} allowDecimals domain={[0, "auto"]} axisLine={false} {...axis} />
        <Tooltip {...tooltipStyle} labelFormatter={v => clock(Number(v))} formatter={(v, name) => [typeof v === "number" ? format(v) : "—", series[Number(String(name).slice(1))]?.label ?? name]} />
        {series.map((s, i) => <Line key={s.label} dataKey={`s${i}`} name={`s${i}`} stroke={s.color} strokeWidth={2} dot={dots && !compact ? { r: 2, fill: s.color, strokeWidth: 0 } : false} activeDot={compact ? false : { r: 4 }} isAnimationActive={false} connectNulls={false} />)}
      </LineChart>
    </ResponsiveContainer>
  </div>;
}

export type CategorySeries<K extends string> = { key: K; label: string; color: string; area?: boolean };

/** Values per bucket (day, hour); areas get a soft fill under the line. */
export function CategoryChart<R extends Record<string, unknown>, K extends Extract<keyof R, string>>({ data, xKey, series, height = 128, label, format = defaultFormat, formatX }: {
  data: R[];
  xKey: Extract<keyof R, string>;
  series: CategorySeries<K>[];
  height?: number;
  label: string;
  format?: (value: number) => string;
  formatX?: (value: string) => string;
}) {
  const gradient = useId().replace(/:/g, "");
  return <div role="img" aria-label={label} style={{ height }}>
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
        <defs>{series.map(s => <linearGradient key={s.key} id={`${gradient}-${s.key}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={s.color} stopOpacity={0.3} />
          <stop offset="100%" stopColor={s.color} stopOpacity={0.02} />
        </linearGradient>)}</defs>
        <CartesianGrid stroke="var(--chart-grid)" vertical={false} />
        <XAxis dataKey={xKey} tickFormatter={formatX} minTickGap={32} {...axis} />
        <YAxis width={40} tickFormatter={format} allowDecimals={false} axisLine={false} {...axis} />
        <Tooltip {...tooltipStyle} labelFormatter={v => formatX ? formatX(String(v)) : String(v)} formatter={(v, name) => [typeof v === "number" ? format(v) : "—", series.find(s => s.key === name)?.label ?? name]} />
        {series.map(s => <Area key={s.key} type="monotone" dataKey={s.key} name={s.key} stroke={s.color} strokeWidth={2} fill={s.area ? `url(#${gradient}-${s.key})` : "transparent"} isAnimationActive={false} />)}
      </AreaChart>
    </ResponsiveContainer>
  </div>;
}
