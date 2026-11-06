import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery } from "@tanstack/react-query";
import { api, getSessionUser } from "../api";
import { PageHeader } from "../components/PageHeader";
import { TradingLimits } from "../components/TradingLimits";
import { DataTable, type TableColumn } from "../components/DataTable";
import { StatusBadge } from "../components/StatusBadge";
import { Icons } from "../components/icons";
import { Field } from "../components/ui/form";
import { useToast } from "../components/Toast";
import { ratePolicySchema, type RatePolicyValues } from "../lib/schemas";

type Policy = { scope: string; subject: string; operation: string; rate: number; burst: number; concurrent: number; enabled: boolean };
type Usage = Policy & { current_rps: number; peak_rps: number; active: number; rejected: number };
type Result = { policies: Policy[]; usage: Usage[]; operations: string[] };
const key = (p: Policy) => `${p.scope}:${p.subject}:${p.operation}`;
const empty: Policy = { scope: "broker", subject: "", operation: "SubmitOrder", rate: 100, burst: 100, concurrent: 16, enabled: true };

export default function RateLimitsPage() {
  const q = useQuery({ queryKey: ["rate-limits"], queryFn: ({ signal }) => api<Result>("/api/v1/admin/rate-limits", { signal }), refetchInterval: 3000 });
  const form = useForm<RatePolicyValues>({ resolver: zodResolver(ratePolicySchema), defaultValues: { ...empty, scope: "broker", reason: "" } });
  const { register, handleSubmit, setValue, watch, formState: { errors, isSubmitting: saving } } = form;
  const draft = watch();
  const [message, setMessage] = useState("");
  // Leaving the page with an unsaved limit asks first.
  const unsaved = form.formState.isDirty && !form.formState.isSubmitSuccessful;
  useEffect(() => {
    if (!unsaved) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [unsaved]);
  const toast = useToast();
  const user = getSessionUser();
  const canEdit = user?.role === "ADMIN" || user?.role === "SUPER_ADMIN";
  const rows = new Map<string, Policy | Usage>();
  q.data?.policies.forEach(p => rows.set(key(p), p));
  q.data?.usage.forEach(p => rows.set(key(p), p));
  const save = handleSubmit(async values => {
    setMessage("");
    try { await api("/api/v1/admin/rate-limits", { method: "PUT", body: JSON.stringify(values) }); setMessage("Лимит сохранён"); toast.push(`Лимит ${values.scope} / ${values.subject || "*"} / ${values.operation} сохранён`, "ok"); await q.refetch(); }
    catch (error) { setMessage(error instanceof Error ? error.message : "Ошибка сохранения"); }
  });
  // The scope decides which subject and operation make sense.
  const scopeField = register("scope", { onChange: e => {
    const scope = e.target.value as RatePolicyValues["scope"];
    setValue("subject", scope === "broker" ? "" : "*");
    setValue("operation", scope === "admin" ? "System" : scope === "global" ? "Trading" : "SubmitOrder");
  } });
  const columns: TableColumn<Policy | Usage>[] = [
    { id: "scope", header: "Область / брокер", value: p => p.scope + " / " + p.subject },
    { id: "operation", header: "Операция", value: p => p.operation, className: "font-mono text-[10px]" },
    { id: "rate", header: "Лимит / с", value: p => p.rate, numeric: true },
    { id: "burst", header: "Burst", value: p => p.burst, numeric: true },
    { id: "concurrent", header: "Параллельные", value: p => p.concurrent, cell: p => ((p as Partial<Usage>).active ?? 0) + " / " + p.concurrent, numeric: true },
    { id: "current", header: "Текущие / с", value: p => (p as Partial<Usage>).current_rps ?? 0, numeric: true },
    { id: "peak", header: "Пик / с", value: p => (p as Partial<Usage>).peak_rps ?? 0, numeric: true },
    { id: "rejected", header: "Отклонено", value: p => (p as Partial<Usage>).rejected ?? 0, numeric: true },
    { id: "status", header: "Статус", value: p => p.enabled ? "ACTIVE" : "DISABLED", cell: p => <StatusBadge status={p.enabled ? "ACTIVE" : "DISABLED"} /> },
    { id: "actions", header: "Действия", value: () => "", sortable: false, cell: p => canEdit && !(p.scope === "broker" && p.subject === "*") ? <button className="btn-muted" onClick={() => { form.reset({ scope: p.scope as RatePolicyValues["scope"], subject: p.scope === "admin" ? "*" : p.subject, operation: p.operation, rate: p.rate, burst: p.burst, concurrent: p.concurrent, enabled: p.enabled, reason: form.getValues("reason") }); setMessage(""); }}>Изменить</button> : null },
  ];
  return <div className="space-y-4">
    <PageHeader title="Rate Limits" subtitle="Лимиты брокеров, операций и глобальная защита" />
    <TradingLimits />
    <p className="mb-4 text-sm text-muted">Статистика текущего Gateway. Current — запросы в текущей секунде. Категории и операции применяются совместно. Изменения вступают в силу без перезапуска.</p>
    {q.error && <p role="alert">{q.error.message}</p>}
    <DataTable urlState title="Лимиты API" rows={[...rows.values()]} columns={columns} rowKey={key} loading={q.isLoading} onRefresh={() => void q.refetch()} refreshing={q.isFetching} summary={list => [{ label: "Правил", value: list.length }, { label: "Включены", value: list.filter(p => p.enabled).length, tone: "success" }]} />
    {canEdit && <form className="mt-6 grid items-start gap-3 rounded-lg border border-line bg-panel p-4 md:grid-cols-3" onSubmit={e => void save(e)} noValidate>
      <Field label="Scope"><select className="input w-full" {...scopeField}>{["broker", "global", "admin"].map(v => <option key={v}>{v}</option>)}</select></Field>
      <Field label="Broker ID" error={errors.subject}><input className="input w-full read-only:cursor-not-allowed read-only:opacity-60" readOnly={draft.scope !== "broker"} aria-disabled={draft.scope !== "broker"} {...register("subject")} /></Field>
      <Field label="Operation" error={errors.operation}><select className="input w-full" {...register("operation")}>{q.data?.operations.filter(v => draft.scope === "global" ? ["*", "Trading", "Queries", "System"].includes(v) : draft.scope === "admin" ? ["*", "System", "Connect", "Subscribe", "Ping"].includes(v) : !["Connect", "Subscribe", "Ping"].includes(v)).map(v => <option key={v}>{v}</option>)}</select></Field>
      {(["rate", "burst", "concurrent"] as const).map(field => <Field key={field} label={field} error={errors[field]}><input className="input w-full" type="number" min={1} max={1000000} {...register(field, { valueAsNumber: true })} /></Field>)}
      <label className="flex items-center gap-2 self-end pb-2 text-sm"><input type="checkbox" {...register("enabled")} />Ограничение включено</label>
      <Field label="Причина изменения" error={errors.reason}><input className="input w-full" maxLength={1000} {...register("reason")} /></Field>
      <button className="btn self-end" disabled={saving}>{saving && <Icons.refresh size={14} className="animate-spin" />}{saving ? "Сохранение…" : "Сохранить"}</button>
      {message && <p role="status" className="md:col-span-3">{message}</p>}
    </form>}
  </div>;
}
