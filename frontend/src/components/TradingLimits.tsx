import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { api, getSessionUser } from "../api";
import { tradingLimitSchema, type TradingLimitValues } from "../lib/schemas";
import { ErrorBanner } from "./feedback";
import { TradingLimitHistory } from "./TradingLimitHistory";
import { Field } from "./ui/form";
import { Icons } from "./icons";
import { useToast } from "./Toast";
import { useConfirm } from "./confirm";

type Policy = { broker_id: number; paused: boolean; max_quantity: string | null; max_open_orders: number | null; revision: number };
const policyName = (p: Policy) => p.broker_id === 0 ? "общей политики" : `брокера №${p.broker_id}`;

export function TradingLimits() {
  const queryClient = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const q = useQuery({ queryKey: ["trading-limits"], queryFn: ({ signal }) => api<{ policies: Policy[] }>("/api/v1/admin/trading-limits", { signal }), refetchInterval: 5000 });
  const [broker, setBroker] = useState("0");
  const [draft, setDraft] = useState<Policy | null>(null);
  const [message, setMessage] = useState("");
  const role = getSessionUser()?.role;
  const canEdit = role === "ADMIN" || role === "SUPER_ADMIN";
  const form = useForm<TradingLimitValues>({ resolver: zodResolver(tradingLimitSchema), defaultValues: { quantity: "", openOrders: "", paused: false, reason: "" } });
  const { register, handleSubmit, reset, formState: { errors, isSubmitting: saving, isDirty } } = form;

  // Leaving the page with an unsaved policy asks first.
  useEffect(() => {
    if (!draft || !isDirty) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [draft, isDirty]);

  function edit(p: Policy) {
    setBroker(String(p.broker_id));
    setDraft(p);
    setMessage("");
    reset({ quantity: p.max_quantity || "", openOrders: p.max_open_orders == null ? "" : String(p.max_open_orders), paused: p.paused, reason: "" });
  }
  const save = handleSubmit(async values => {
    if (!draft) return;
    // Pausing stops every new order of the broker (or of all brokers): ask first.
    if (values.paused && !draft.paused && !(await confirm({
      title: draft.broker_id === 0 ? "Приостановить приём заявок всех брокеров?" : `Приостановить приём заявок брокера №${draft.broker_id}?`,
      description: "Новые заявки будут отклоняться, пока паузу не снимут. Уже принятые заявки продолжают обрабатываться, отмены доступны.",
      confirmLabel: "Приостановить", danger: true,
    }))) return;
    setMessage("");
    try {
      await api("/api/v1/admin/trading-limits", { method: "PUT", body: JSON.stringify({ broker_id: draft.broker_id, paused: values.paused, revision: draft.revision, max_quantity: values.quantity.trim() || null, max_open_orders: values.openOrders.trim() ? Number(values.openOrders) : null, reason: values.reason }) });
      setMessage("Торговый лимит сохранён");
      toast.push(`Лимит ${policyName(draft)} сохранён`, "ok");
      setDraft(null);
      await q.refetch();
      await queryClient.invalidateQueries({ queryKey: ["trading-limit-history"] });
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Ошибка сохранения");
    }
  });

  return <section className="card my-6 space-y-3">
    <h2 className="font-medium">Торговые ограничения</h2>
    <p className="text-sm text-muted">Общий и персональный лимиты действуют совместно. Пауза блокирует приём новых заявок. Уже принятые заявки продолжают обрабатываться; отмены доступны. Общий лимит открытых заявок применяется к каждому брокеру отдельно.</p>
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    {q.isLoading && <p>Загрузка…</p>}
    {q.data?.policies.map(p => <div key={p.broker_id} className="flex flex-wrap items-center gap-3 border-b border-line py-2 text-sm">
      <strong>{p.broker_id === 0 ? "Все брокеры" : `Брокер №${p.broker_id}`}</strong>
      <span className={p.paused ? "font-medium text-warning" : ""}>{p.paused ? "Приём приостановлен" : "Приём разрешён"}</span>
      <span>Количество: {p.max_quantity ?? "без ограничения"}</span>
      <span>Открытых заявок: {p.max_open_orders ?? "без ограничения"}</span>
      {canEdit && <button type="button" className="btn" onClick={() => edit(p)}>Изменить</button>}
    </div>)}
    {canEdit && <div className="flex flex-wrap gap-2">
      <input className="input w-40" type="number" min={1} aria-label="ID брокера для персонального лимита" value={broker} onChange={e => setBroker(e.target.value)} />
      <button type="button" className="btn" disabled={!/^\d+$/.test(broker) || !Number.isSafeInteger(Number(broker)) || saving || !q.data} onClick={() => edit(q.data?.policies.find(p => p.broker_id === Number(broker)) || { broker_id: Number(broker), paused: false, max_quantity: null, max_open_orders: null, revision: 0 })}>Настроить брокера</button>
    </div>}
    {canEdit && draft && <form className="grid items-start gap-3 md:grid-cols-2" onSubmit={e => void save(e)} noValidate aria-label={`Политика ${policyName(draft)}`}>
      <p className="md:col-span-2">{draft.broker_id === 0 ? "Общая политика" : `Политика брокера №${draft.broker_id}`}</p>
      <Field label="Максимальное количество" error={errors.quantity}><input className="input w-full" inputMode="decimal" placeholder="Без ограничения" {...register("quantity")} /></Field>
      <Field label="Максимум открытых заявок" error={errors.openOrders}><input className="input w-full" inputMode="numeric" placeholder="Без ограничения" {...register("openOrders")} /></Field>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" {...register("paused")} />Приостановить приём новых заявок</label>
      <Field label="Причина изменения" error={errors.reason}><input className="input w-full" maxLength={1000} {...register("reason")} /></Field>
      <div className="flex gap-2 md:col-span-2">
        <button className="btn" disabled={saving}>{saving && <Icons.refresh size={14} className="animate-spin" />}{saving ? "Сохранение…" : "Сохранить"}</button>
        <button type="button" className="btn-muted" disabled={saving} onClick={() => { setDraft(null); setMessage(""); }}>Отмена</button>
      </div>
    </form>}
    {message && <p role="status">{message}</p>}
    <TradingLimitHistory />
  </section>;
}
