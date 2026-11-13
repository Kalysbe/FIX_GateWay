import { useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Icons } from "./icons";
import { useToast } from "./Toast";
import { useConfirm, type ConfirmOptions } from "./confirm";

/**
 * A button for a server action: it spins and stays disabled while the request
 * runs, so a second click cannot send it twice, and reports the outcome in a toast.
 * With `confirm` the action runs from the confirmation dialog instead.
 */
export function ActionButton({ onAction, success, failure = "Не удалось выполнить действие", confirm, children, className = "btn-muted", disabled, ...rest }: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onClick"> & {
  onAction: () => Promise<unknown>;
  success?: string;
  failure?: string;
  confirm?: Omit<ConfirmOptions, "action" | "success">;
  children: ReactNode;
}) {
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const ask = useConfirm();
  async function run() {
    if (confirm) {
      await ask({ ...confirm, action: onAction, success });
      return;
    }
    setBusy(true);
    try {
      await onAction();
      if (success) toast.push(success, "ok");
    } catch (error) {
      toast.push(error instanceof Error && error.message ? error.message : failure);
    } finally {
      setBusy(false);
    }
  }
  return <button type="button" {...rest} className={`${className} gap-1.5`} disabled={disabled || busy} aria-busy={busy || undefined} onClick={() => void run()}>
    {busy && <Icons.refresh size={13} className="animate-spin" />}
    {children}
  </button>;
}
