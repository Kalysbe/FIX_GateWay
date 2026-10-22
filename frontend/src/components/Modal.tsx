import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Icons } from "./icons";

export function Modal({
  title,
  onClose,
  children,
  wide,
  motion = true,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
  motion?: boolean;
}) {
  // Capture the trigger before the focus trap moves focus into the dialog.
  const [animate] = useState(() => motion && !document.activeElement?.matches(":focus-visible"));
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const lastFocus = useRef<HTMLElement | null>(null);
  const closeRef = useRef(onClose);
  useEffect(() => { closeRef.current = onClose; }, [onClose]);

  useEffect(() => {
    lastFocus.current = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    const focusable = panel?.querySelector<HTMLElement>('[data-autofocus]') ?? panel?.querySelector<HTMLElement>('input:not(:disabled), select:not(:disabled), textarea:not(:disabled)') ?? panel?.querySelector<HTMLElement>(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
    );
    focusable?.focus();

    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        closeRef.current();
        return;
      }
      if (e.key !== "Tab" || !panel) return;
      const nodes = Array.from(
        panel.querySelectorAll<HTMLElement>('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'),
      ).filter((el) => !el.hasAttribute("disabled"));
      if (!nodes.length) return;
      const first = nodes[0];
      const last = nodes[nodes.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }

    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      lastFocus.current?.focus();
    };
  }, []);

  return (
    <div className="modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4" data-motion={animate ? undefined : "instant"} onClick={onClose}>
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`modal-panel card max-h-[90vh] w-full overflow-y-auto ${wide ? "max-w-2xl" : "max-w-lg"}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between gap-3">
          <h3 id={titleId} className="text-lg font-semibold">
            {title}
          </h3>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Закрыть">
            <Icons.x size={16} />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
