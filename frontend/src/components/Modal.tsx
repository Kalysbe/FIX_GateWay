import { useEffect, useRef, useState, type ReactNode } from "react";
import { cn } from "../lib/utils";
import { useConfirm } from "./confirm";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";

export function Modal({
  title,
  onClose,
  children,
  wide,
  motion = true,
  bare = false,
  dirty = false,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
  motion?: boolean;
  /** Content draws its own padding and the title stays for screen readers only. */
  bare?: boolean;
  /**
   * The form inside has unsaved input: Esc, a click outside and the close button
   * ask before discarding it, and leaving the page asks too. An explicit
   * "Отмена" button in the form still closes at once.
   */
  dirty?: boolean;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const confirm = useConfirm();
  // Pages mount the dialog without a Radix trigger; return focus to what opened it.
  const [opener] = useState(() => document.activeElement as HTMLElement | null);

  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  async function requestClose() {
    if (dirty && !(await confirm({ title: "Закрыть без сохранения?", description: "Введённые в форму данные пропадут.", confirmLabel: "Не сохранять", danger: true }))) return;
    onClose();
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) void requestClose(); }}>
      <DialogContent
        ref={panelRef}
        aria-describedby={undefined}
        showCloseButton={!bare}
        className={cn(wide ? "max-w-2xl" : "max-w-lg", !motion && "duration-0 data-[state=open]:animate-none", bare && "gap-0 p-0")}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          if (opener?.isConnected) opener.focus();
        }}
        onOpenAutoFocus={(event) => {
          // Prefer the field the dialog marks, then the first form control.
          const panel = panelRef.current;
          const target =
            panel?.querySelector<HTMLElement>("[data-autofocus]") ??
            panel?.querySelector<HTMLElement>("input:not(:disabled), select:not(:disabled), textarea:not(:disabled)");
          if (target) {
            event.preventDefault();
            target.focus();
          }
        }}
      >
        <DialogHeader className={cn(bare && "sr-only")}>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="min-w-0">{children}</div>
      </DialogContent>
    </Dialog>
  );
}
