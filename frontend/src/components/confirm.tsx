import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { Modal } from "./Modal";
import { Icons } from "./icons";
import { useToast } from "./Toast";

export type ConfirmOptions = {
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  /** Red confirm button for destructive or access-cutting actions. */
  danger?: boolean;
  /** Runs inside the dialog: the button spins, an error stays in the dialog. */
  action?: () => Promise<unknown>;
  /** Toast after the action succeeds. */
  success?: string;
};

type Pending = ConfirmOptions & { resolve: (ok: boolean) => void };
const Ctx = createContext<(options: ConfirmOptions) => Promise<boolean>>(async () => false);

/** `await confirm({...})` is true when the user confirmed (and the action, if any, succeeded). */
export function useConfirm() {
  return useContext(Ctx);
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();
  const current = useRef<Pending | null>(null);

  const confirm = useCallback((options: ConfirmOptions) => new Promise<boolean>(resolve => {
    current.current?.resolve(false);
    const next = { ...options, resolve };
    current.current = next;
    setError("");
    setBusy(false);
    setPending(next);
  }), []);

  function close(ok: boolean) {
    current.current?.resolve(ok);
    current.current = null;
    setPending(null);
  }
  async function accept() {
    if (!pending) return;
    if (!pending.action) return close(true);
    setBusy(true);
    setError("");
    try {
      await pending.action();
      if (pending.success) toast.push(pending.success, "ok");
      close(true);
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : "Не удалось выполнить действие");
    } finally {
      setBusy(false);
    }
  }

  return <Ctx.Provider value={confirm}>
    {children}
    {pending && <Modal title={pending.title} onClose={() => { if (!busy) close(false); }}>
      {pending.description && <div className="text-sm text-muted">{pending.description}</div>}
      {error && <p role="alert" className="mt-3 rounded-md border border-danger/20 bg-danger-soft px-3 py-2 text-sm text-danger">{error}</p>}
      <div className="mt-5 flex justify-end gap-2">
        <button type="button" className="btn-muted" disabled={busy} onClick={() => close(false)}>Отмена</button>
        <button type="button" data-autofocus className={pending.danger ? "btn btn-danger gap-1.5" : "btn gap-1.5"} disabled={busy} onClick={() => void accept()}>
          {busy && <Icons.refresh size={14} className="animate-spin" />}
          {pending.confirmLabel ?? "Подтвердить"}
        </button>
      </div>
    </Modal>}
  </Ctx.Provider>;
}
