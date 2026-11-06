import { useRef, useState, type ReactNode } from "react";
import { cn } from "../lib/utils";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";

export function Modal({
  title,
  onClose,
  children,
  wide,
  motion = true,
  bare = false,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
  motion?: boolean;
  /** Content draws its own padding and the title stays for screen readers only. */
  bare?: boolean;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  // Pages mount the dialog without a Radix trigger; return focus to what opened it.
  const [opener] = useState(() => document.activeElement as HTMLElement | null);
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
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
