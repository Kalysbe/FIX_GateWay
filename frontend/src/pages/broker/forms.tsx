import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { errorText, Field, FormError } from "../../components/ui/form";
import { brokerCreateSchema, brokerEditSchema, ipEntrySchema, type BrokerCreateValues, type BrokerEditValues, type IPEntryValues } from "../../lib/schemas";

export function BrokerCreateForm({ onCreate, onCancel }: { onCreate: (values: BrokerCreateValues) => Promise<unknown>; onCancel: () => void }) {
  const { register, handleSubmit, setError, formState: { errors, isSubmitting } } = useForm<BrokerCreateValues>({ resolver: zodResolver(brokerCreateSchema), defaultValues: { code: "", name: "" } });
  const submit = handleSubmit(async values => {
    try { await onCreate(values); } catch (error) { setError("root", { message: errorText(error, "Не удалось создать брокера") }); }
  });
  return <form onSubmit={submit} className="space-y-3" noValidate>
    <Field label="Код" error={errors.code}><input className="input" placeholder="Код" data-autofocus {...register("code")} /></Field>
    <Field label="Название" error={errors.name}><input className="input" placeholder="Название" {...register("name")} /></Field>
    <FormError error={errors.root} />
    <div className="flex justify-end gap-2">
      <button type="button" className="btn-muted" onClick={onCancel}>Отмена</button>
      <button className="btn" type="submit" disabled={isSubmitting}>Создать</button>
    </div>
  </form>;
}

export function BrokerEditForm({ initial, onSave, onCancel }: { initial: BrokerEditValues; onSave: (values: BrokerEditValues) => Promise<unknown>; onCancel: () => void }) {
  const { register, handleSubmit, setError, formState: { errors, isSubmitting } } = useForm<BrokerEditValues>({ resolver: zodResolver(brokerEditSchema), defaultValues: initial });
  const submit = handleSubmit(async values => {
    try { await onSave({ name: values.name.trim(), description: values.description.trim() }); } catch (error) { setError("root", { message: errorText(error, "Не удалось сохранить брокера") }); }
  });
  return <form className="space-y-3" onSubmit={submit} noValidate>
    <Field label="Название" error={errors.name}><input className="input" {...register("name")} /></Field>
    <Field label="Описание" error={errors.description}><textarea className="input min-h-[80px]" {...register("description")} /></Field>
    <FormError error={errors.root} />
    <div className="flex justify-end gap-2">
      <button type="button" className="btn-muted" onClick={onCancel}>Отмена</button>
      <button type="submit" className="btn" disabled={isSubmitting}>Сохранить</button>
    </div>
  </form>;
}

export function IPEntryForm({ onAdd }: { onAdd: (values: IPEntryValues) => Promise<unknown> }) {
  const { register, handleSubmit, reset, setError, formState: { errors, isSubmitting } } = useForm<IPEntryValues>({ resolver: zodResolver(ipEntrySchema), defaultValues: { ip: "", note: "" } });
  const submit = handleSubmit(async values => {
    try { await onAdd(values); reset(); } catch (error) { setError("root", { message: errorText(error, "Не удалось добавить адрес") }); }
  });
  return <form onSubmit={submit} className="mb-3 space-y-2" noValidate>
    <div className="grid items-start gap-2 sm:grid-cols-[1fr_1fr_auto]">
      <Field label="IP / CIDR" labelClassName="sr-only" error={errors.ip} className="space-y-1"><input className="input" placeholder="185.10.10.10 или CIDR" {...register("ip")} /></Field>
      <Field label="Описание" labelClassName="sr-only" className="space-y-1"><input className="input" placeholder="Описание" {...register("note")} /></Field>
      <button className="btn" type="submit" disabled={isSubmitting}>Добавить</button>
    </div>
    <FormError error={errors.root} />
  </form>;
}
