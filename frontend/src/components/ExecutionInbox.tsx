import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { api, getSessionUser } from "../api";
import { formatDateTime } from "../labels";
import { ErrorBanner } from "./feedback";
import { ActionButton } from "./ActionButton";
import { useConfirm } from "./confirm";
import { Field } from "./ui/form";
import { Icons } from "./icons";
import { quarantineSchema, type QuarantineValues } from "../lib/schemas";

type Report = {
  adapter_id: string; cl_ord_id: string; orig_cl_ord_id: string; order_id: string; exec_id: string;
  exec_type: string; exec_trans_type: string; ord_status: string; last_qty: string; last_px: string;
  cum_qty: string; leaves_qty: string; text: string; protocol_reject: boolean; ref_msg_type: string;
  ref_seq_num: string; cancel_reject: boolean; raw_fix: string;
};
type Blocked = {
  entry_id: string; entry_name: string; error: string; failed_at: string; attempts: number;
  read_error: boolean; received_at?: string; report?: Report;
};
type Case = {
  entry_id: string; entry_name: string; received_at: string | null; adapter_id: string; cl_ord_id: string;
  exec_id: string; exec_type: string; order_id: string | null; apply_error: string; attempts: number;
  reason: string; quarantined_by: string; quarantined_at: string;
  resolved_by: string | null; resolved_at: string | null; resolution_note: string | null;
};
export type InboxState = {
  status: { pending: number; bytes: number; failed: boolean; healthy: boolean; oldest_age_seconds: number };
  blocked: Blocked | null;
  cases: Case[];
};

function kind(r?: Report) {
  if (!r) return "Файл отчёта повреждён";
  if (r.protocol_reject) return `Отклонение FIX (35=3/j) · ${r.ref_msg_type || "тип не указан"} · seq ${r.ref_seq_num || "—"}`;
  if (r.cancel_reject) return "Отказ в отмене (35=9)";
  return `ExecutionReport · 150=${r.exec_type || "—"} · 39=${r.ord_status || "—"}${r.exec_trans_type && r.exec_trans_type !== "0" ? ` · 20=${r.exec_trans_type}` : ""}`;
}

export function ExecutionInbox() {
  const confirm = useConfirm();
  const quarantineForm = useForm<QuarantineValues>({ resolver: zodResolver(quarantineSchema), defaultValues: { reason: "" } });
  const [notes, setNotes] = useState<Record<string, string>>({});
  const role = getSessionUser()?.role;
  const canAct = ["SUPER_ADMIN", "ADMIN", "OPERATOR"].includes(role || "");
  const q = useQuery({
    queryKey: ["execution-inbox"],
    queryFn: ({ signal }) => api<InboxState>("/api/v1/admin/execution-inbox", { signal }),
    refetchInterval: 3000,
  });
  const quarantine = useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) => api(`/api/v1/admin/execution-inbox/${encodeURIComponent(id)}/quarantine`, {
      method: "POST", body: JSON.stringify({ reason }),
    }),
    onSuccess: () => { quarantineForm.reset(); void q.refetch(); },
  });
  const resolve = useMutation({
    mutationFn: ({ id, note }: { id: string; note: string }) => api(`/api/v1/admin/execution-inbox/cases/${encodeURIComponent(id)}/resolve`, {
      method: "POST", body: JSON.stringify({ note }),
    }),
    onSuccess: (_, { id }) => { setNotes(old => { const next = { ...old }; delete next[id]; return next; }); void q.refetch(); },
  });
  const s = q.data?.status;
  const b = q.data?.blocked;
  const cases = q.data?.cases || [];
  return <div className="space-y-4">
    {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
    {q.isLoading && <p className="text-sm">Загрузка…</p>}
    {s && <section className="card space-y-1 text-sm" role="status">
      <h2 className="font-medium">{s.healthy ? "Входящие FIX-отчёты обрабатываются" : "Обработка входящих FIX-отчётов остановлена"}</h2>
      <p className="text-muted">В очереди: {s.pending} · старейший: {Math.round(s.oldest_age_seconds)} с{s.healthy ? "" : " · новые заявки и отправка приостановлены"}</p>
    </section>}
    {b && <section className="card space-y-3 text-sm">
      <h2 className="font-medium">Блокирующий отчёт</h2>
      <dl className="grid gap-x-4 gap-y-1 sm:grid-cols-[max-content_1fr]">
        <dt className="text-muted">Тип</dt><dd>{kind(b.report)}</dd>
        {b.report && <><dt className="text-muted">Адаптер</dt><dd>{b.report.adapter_id || "—"}</dd>
          <dt className="text-muted">ClOrdID / ExecID</dt><dd className="break-all">{b.report.cl_ord_id || "—"} / {b.report.exec_id || "—"}</dd>
          {b.report.text && <><dt className="text-muted">Текст (58)</dt><dd className="break-words">{b.report.text}</dd></>}</>}
        <dt className="text-muted">Получен</dt><dd>{b.received_at ? formatDateTime(b.received_at) : "—"}</dd>
        <dt className="text-muted">Ошибка</dt><dd className="break-words">{b.error}</dd>
        <dt className="text-muted">Попыток</dt><dd>{b.attempts} · с {formatDateTime(b.failed_at)}</dd>
      </dl>
      {b.report?.raw_fix && <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded bg-subtle p-2 text-xs">{b.report.raw_fix}</pre>}
      <p className="text-xs text-muted">Если ошибка временная (например, недоступна БД), дождитесь повторной попытки. Карантин не применяет отчёт к заявкам и сделкам: он откладывается как случай ручной сверки, а следующие отчёты обрабатываются. Файл сохраняется в каталоге quarantine.</p>
      {canAct && <form className="flex flex-wrap items-start gap-2" noValidate onSubmit={quarantineForm.handleSubmit(async ({ reason }) => {
        await confirm({
          title: "Отложить отчёт в карантин?",
          description: <>Отчёт {b.report?.cl_ord_id ? <span className="font-mono text-ink">{b.report.cl_ord_id}</span> : ""} не будет применён к заявкам и сделкам: он станет случаем ручной сверки, а следующие отчёты начнут обрабатываться. Файл сохранится в каталоге quarantine.</>,
          confirmLabel: "Отложить в карантин", danger: true,
          action: () => quarantine.mutateAsync({ id: b.entry_id, reason: reason.trim() }),
          success: "Отчёт отложен в карантин, обработка продолжена",
        });
      })}>
        <Field label="Причина карантина" labelClassName="sr-only" error={quarantineForm.formState.errors.reason} className="flex-1 space-y-1"><input className="input" placeholder="Причина и план сверки" maxLength={2000} {...quarantineForm.register("reason")} /></Field>
        <button className="btn" type="submit" disabled={quarantine.isPending}>{quarantine.isPending && <Icons.refresh size={14} className="animate-spin" />}Отложить в карантин</button>
      </form>}
    </section>}
    <section className="card space-y-3 text-sm">
      <h2 className="font-medium">Случаи карантина</h2>
      {!q.isLoading && cases.length === 0 && <p className="text-muted">Отложенных отчётов нет.</p>}
      {cases.map(c => <div key={c.entry_id} className="rounded-lg border border-line p-3 space-y-1">
        <div>{c.adapter_id || "—"} · {c.cl_ord_id || "без ClOrdID"}{c.exec_type ? ` · 150=${c.exec_type}` : ""}{c.order_id ? ` · заявка ${c.order_id}` : ""}</div>
        <p className="break-words text-muted">{c.apply_error}</p>
        <p>Отложен {formatDateTime(c.quarantined_at)} · {c.quarantined_by} · {c.reason}</p>
        {c.resolved_at ? <p>Закрыт {formatDateTime(c.resolved_at)} · {c.resolved_by} · {c.resolution_note}</p> : canAct && <div className="flex flex-wrap gap-2">
          <input className="input flex-1" aria-label={`Итог сверки ${c.entry_id}`} placeholder="Итог ручной сверки" maxLength={2000} value={notes[c.entry_id] || ""} onChange={e => setNotes({ ...notes, [c.entry_id]: e.target.value })} />
          <ActionButton className="btn" disabled={!(notes[c.entry_id] || "").trim()} onAction={() => resolve.mutateAsync({ id: c.entry_id, note: notes[c.entry_id].trim() })} success="Случай карантина закрыт" failure="Не удалось закрыть случай"
            confirm={{ title: "Закрыть случай карантина?", description: <>Итог сверки сохранится в истории: «{(notes[c.entry_id] || "").trim()}».</>, confirmLabel: "Закрыть случай" }}>Закрыть случай</ActionButton>
        </div>}
      </div>)}
    </section>
  </div>;
}
