import { useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, apiDownload } from "../api";
import { formatDateTime, formatMs, formatNumber, logTypeLabel } from "../labels";
import { useBreadcrumbs } from "../components/breadcrumbs";

export type LogRow = {
  id: string;
  timestamp: string;
  type: string;
  subtype: string;
  severity: string;
  request_id?: string;
  trace_id?: string;
  broker_id?: number | null;
  broker_name?: string;
  token_id?: string;
  client_ip?: string;
  user_agent?: string;
  http_method?: string;
  endpoint?: string;
  status_code?: number | null;
  response_time_ms?: number | null;
  result?: string;
  order_id?: string;
  client_order_id?: string;
  account_id?: string;
  instrument?: string;
  side?: string;
  quantity?: string;
  price?: string;
  adapter_id?: string;
  session_id?: string;
  fix_msg_type?: string;
  fix_direction?: string;
  fix_seq_num?: number | null;
  error_code?: string;
  error_message?: string;
  failure_stage?: string;
  message?: string;
};

const KINDS: { id: string; label: string }[] = [
  { id: "all", label: "Все" },
  { id: "api", label: "API" },
  { id: "orders", label: "Заявки" },
  { id: "errors", label: "Ошибки" },
  { id: "auth", label: "Авторизация" },
  { id: "fix", label: "FIX / Adapters" },
  { id: "system", label: "Система" },
  { id: "audit", label: "Audit" },
];

function severityClass(s?: string) {
  switch ((s || "").toUpperCase()) {
    case "CRITICAL":
      return "badge-danger";
    case "ERROR":
      return "badge-danger";
    case "WARNING":
      return "badge-warn";
    case "DEBUG":
      return "badge-muted";
    default:
      return "badge-info";
  }
}

function isoDayStart() {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d.toISOString();
}

export default function LogsPage() {
  const { kind: kindParam } = useParams();
  const [params, setParams] = useSearchParams();
  const kind = kindParam || params.get("kind") || "all";
  const brokerId = params.get("broker_id") || "";

  const [from, setFrom] = useState(isoDayStart());
  const [to, setTo] = useState("");
  const [severity, setSeverity] = useState("");
  const [ip, setIp] = useState("");
  const [requestId, setRequestId] = useState("");
  const [orderId, setOrderId] = useState("");
  const [clientOrderId, setClientOrderId] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [method, setMethod] = useState("");
  const [adapter, setAdapter] = useState("");
  const [errorCode, setErrorCode] = useState(params.get("error_code") || "");
  const [cursor, setCursor] = useState("");
  const [selected, setSelected] = useState<LogRow | null>(null);
  const [pages, setPages] = useState<string[]>([""]);

  useBreadcrumbs([{ label: "Логи" }, { label: KINDS.find((k) => k.id === kind)?.label || "Все" }]);

  const qs = useMemo(() => {
    const q = new URLSearchParams();
    if (kind && kind !== "all") q.set("kind", kind);
    if (brokerId) q.set("broker_id", brokerId);
    if (from) q.set("from", from);
    if (to) q.set("to", to);
    if (severity) q.set("severity", severity);
    if (ip) q.set("ip", ip);
    if (requestId) q.set("request_id", requestId);
    if (orderId) q.set("order_id", orderId);
    if (clientOrderId) q.set("client_order_id", clientOrderId);
    if (endpoint) q.set("endpoint", endpoint);
    if (method) q.set("http_method", method);
    if (adapter) q.set("adapter_id", adapter);
    if (errorCode) q.set("error_code", errorCode);
    if (cursor) q.set("cursor", cursor);
    q.set("limit", "20");
    return q.toString();
  }, [kind, brokerId, from, to, severity, ip, requestId, orderId, clientOrderId, endpoint, method, adapter, errorCode, cursor]);

  const q = useQuery({
    queryKey: ["logs", qs],
    queryFn: () => api<{ logs: LogRow[]; next_cursor?: string; total?: number }>(`/api/v1/admin/logs?${qs}`),
    refetchInterval: 8000,
  });

  const brokers = useQuery({
    queryKey: ["brokers"],
    queryFn: () => api<{ brokers: Array<{ id: number; name: string }> }>("/api/v1/admin/brokers"),
  });

  useEffect(() => {
    setCursor("");
    setPages([""]);
  }, [kind, brokerId, from, to, severity, ip, requestId, orderId, clientOrderId, endpoint, method, adapter, errorCode]);

  const rows = q.data?.logs || [];

  async function exportLogs(format: "csv" | "json") {
    const p = new URLSearchParams(qs);
    p.delete("cursor");
    p.set("format", format);
    const res = await apiDownload(`/api/v1/admin/logs/export?${p.toString()}`);
    if (res.json?.job?.id) {
      alert("Экспорт запущен асинхронно. Обновите страницу уведомлений позже.");
      return;
    }
    const url = URL.createObjectURL(res.blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = res.filename || `logs.${format}`;
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Логи</h1>
          <p className="text-sm text-slate-500">
            {formatNumber(q.data?.total)} событий · фильтры применяются на сервере
          </p>
        </div>
        <div className="flex gap-2">
          <button type="button" className="btn-muted" onClick={() => exportLogs("csv")}>
            CSV
          </button>
          <button type="button" className="btn-muted" onClick={() => exportLogs("json")}>
            JSON
          </button>
        </div>
      </div>

      <div className="flex flex-wrap gap-1">
        {KINDS.map((k) => (
          <Link
            key={k.id}
            to={k.id === "all" ? "/logs" : `/logs/${k.id}`}
            className={kind === k.id ? "btn" : "btn-muted"}
          >
            {k.label}
          </Link>
        ))}
      </div>

      <div className="card flex flex-wrap items-center gap-2">
        <input className="input w-auto" type="datetime-local" value={toLocal(from)} onChange={(e) => setFrom(fromLocal(e.target.value))} />
        <input className="input w-auto" type="datetime-local" value={toLocal(to)} onChange={(e) => setTo(fromLocal(e.target.value))} />
        <select className="input w-auto" value={brokerId} onChange={(e) => setParams(mergeParam(params, "broker_id", e.target.value))}>
          <option value="">Все фирмы</option>
          {(brokers.data?.brokers || []).map((b) => (
            <option key={b.id} value={b.id}>
              {b.name}
            </option>
          ))}
        </select>
        <select className="input w-auto" value={severity} onChange={(e) => setSeverity(e.target.value)}>
          <option value="">Severity</option>
          {["DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"].map((s) => (
            <option key={s}>{s}</option>
          ))}
        </select>
        <select className="input w-auto" value={method} onChange={(e) => setMethod(e.target.value)}>
          <option value="">Метод</option>
          {["GET", "POST", "PATCH", "DELETE"].map((s) => (
            <option key={s}>{s}</option>
          ))}
        </select>
        <input className="input min-w-[140px] flex-1" placeholder="IP" value={ip} onChange={(e) => setIp(e.target.value)} />
        <input className="input min-w-[160px] flex-1" placeholder="Request ID" value={requestId} onChange={(e) => setRequestId(e.target.value)} />
        <input className="input min-w-[140px]" placeholder="Order ID" value={orderId} onChange={(e) => setOrderId(e.target.value)} />
        <input className="input min-w-[140px]" placeholder="Client Order ID" value={clientOrderId} onChange={(e) => setClientOrderId(e.target.value)} />
        <input className="input min-w-[140px]" placeholder="Endpoint" value={endpoint} onChange={(e) => setEndpoint(e.target.value)} />
        <input className="input min-w-[120px]" placeholder="Adapter" value={adapter} onChange={(e) => setAdapter(e.target.value)} />
      </div>

      <div className="card !p-0 overflow-x-auto">
        <table className="table">
          <thead>
            <tr>
              <th>Время</th>
              <th>Severity</th>
              <th>Тип</th>
              <th>Подтип</th>
              <th>Фирма</th>
              <th>IP</th>
              <th>Endpoint</th>
              <th>Status</th>
              <th>Duration</th>
              <th>Request ID</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id} className="cursor-pointer hover:bg-slate-50" onClick={() => setSelected(row)}>
                <td className="whitespace-nowrap font-mono text-xs">{formatDateTime(row.timestamp)}</td>
                <td>
                  <span className={severityClass(row.severity)}>{row.severity}</span>
                </td>
                <td>{logTypeLabel(row.type)}</td>
                <td className="font-mono text-xs">{row.subtype}</td>
                <td>{row.broker_name || row.broker_id || "—"}</td>
                <td className="font-mono text-xs">{row.client_ip || "—"}</td>
                <td className="font-mono text-xs">
                  {row.http_method ? `${row.http_method} ` : ""}
                  {row.endpoint || "—"}
                </td>
                <td>{row.status_code ?? row.result ?? "—"}</td>
                <td>{formatMs(row.response_time_ms)}</td>
                <td className="font-mono text-[11px] text-slate-500">{row.request_id || "—"}</td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={10} className="py-10 text-center text-slate-400">
                  {q.isLoading ? "Загрузка…" : "Нет событий по выбранным фильтрам"}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
        <div className="text-slate-500">
          Страница <span className="font-mono text-ink">{pages.length}</span>
          {q.data?.total != null && (
            <>
              {" "}
              · всего <span className="font-mono text-ink">{formatNumber(q.data.total)}</span>
            </>
          )}
          , по 20
        </div>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            className="btn-muted"
            disabled={pages.length <= 1}
            onClick={() => {
              const next = pages.slice(0, -1);
              setPages(next);
              setCursor(next[next.length - 1] || "");
            }}
          >
            Назад
          </button>
          <button
            type="button"
            className="btn-muted"
            disabled={!q.data?.next_cursor}
            onClick={() => {
              const c = q.data?.next_cursor || "";
              setPages((p) => [...p, c]);
              setCursor(c);
            }}
          >
            Далее
          </button>
        </div>
      </div>

      {selected && <LogDetails row={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}

function LogDetails({ row, onClose }: { row: LogRow; onClose: () => void }) {
  const q = useQuery({
    queryKey: ["log", row.id],
    queryFn: () => api<{ log: LogRow }>(`/api/v1/admin/logs/${row.id}`),
  });
  const d = q.data?.log || row;
  return (
    <div className="fixed inset-y-0 right-0 left-[232px] z-50 flex items-start justify-end bg-slate-900/40" onClick={onClose}>
      <div className="h-full w-full max-w-lg overflow-y-auto bg-white p-5 shadow-xl" onClick={(e) => e.stopPropagation()}>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Детали события</h2>
          <button type="button" className="btn-muted" onClick={onClose}>
            Закрыть
          </button>
        </div>
        <dl className="space-y-2 text-sm">
          <Row k="Timestamp" v={formatDateTime(d.timestamp)} />
          <Row k="Request ID" v={d.request_id} mono />
          <Row k="Trace ID" v={d.trace_id} mono />
          <Row k="Broker" v={d.broker_name || String(d.broker_id || "—")} />
          <Row k="Token ID" v={d.token_id} mono />
          <Row k="IP" v={d.client_ip} mono />
          <Row k="User Agent" v={d.user_agent} />
          <Row k="HTTP" v={`${d.http_method || ""} ${d.endpoint || ""}`.trim() || "—"} />
          <Row k="Status" v={String(d.status_code ?? d.result ?? "—")} />
          <Row k="Duration" v={formatMs(d.response_time_ms)} />
          <Row k="Type" v={`${logTypeLabel(d.type)} / ${d.subtype}`} />
          <Row k="Severity" v={d.severity} />
          {d.order_id && <Row k="Order ID" v={d.order_id} mono />}
          {d.client_order_id && <Row k="Client Order ID" v={d.client_order_id} mono />}
          {d.account_id && <Row k="Account" v={d.account_id} />}
          {d.instrument && <Row k="Instrument" v={`${d.instrument} ${d.side || ""} ${d.quantity || ""} @ ${d.price || ""}`} />}
          {d.adapter_id && <Row k="Adapter" v={d.adapter_id} />}
          {d.session_id && <Row k="Session" v={d.session_id} />}
          {d.fix_msg_type && <Row k="FIX" v={`${d.fix_direction || ""} ${d.fix_msg_type} seq=${d.fix_seq_num ?? "—"}`} />}
          {d.error_code && <Row k="Error" v={`${d.error_code}: ${d.error_message || ""}`} />}
          {d.failure_stage && <Row k="Stage" v={d.failure_stage} />}
          {d.message && <Row k="Message" v={d.message} />}
        </dl>
        {d.order_id && (
          <Link to={`/orders/${d.order_id}`} className="mt-4 inline-block text-sm font-medium text-accent">
            Открыть заявку и полный trace →
          </Link>
        )}
      </div>
    </div>
  );
}

function Row({ k, v, mono }: { k: string; v?: string | null; mono?: boolean }) {
  return (
    <div className="flex justify-between gap-3 border-b border-slate-50 py-1.5">
      <dt className="text-slate-500">{k}</dt>
      <dd className={`text-right ${mono ? "font-mono text-xs" : ""}`}>{v || "—"}</dd>
    </div>
  );
}

function toLocal(iso: string) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocal(v: string) {
  if (!v) return "";
  return new Date(v).toISOString();
}

function mergeParam(params: URLSearchParams, key: string, value: string) {
  const next = new URLSearchParams(params);
  if (value) next.set(key, value);
  else next.delete(key);
  return next;
}
