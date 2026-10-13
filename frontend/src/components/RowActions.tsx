import { useLayoutEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Icons } from "./icons";
import { useToast } from "./Toast";

export function RowActions({ label, actions }: { label: string; actions: { label: string; run: () => void | Promise<void>; danger?: boolean }[] }) {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ left: 0, top: 0 });
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const toast = useToast();
  const id = useId();
  useLayoutEffect(() => {
    if (!open) return;
    const rect = button.current!.getBoundingClientRect();
    const height = 12 + actions.length * 34;
    setPosition({ left: Math.max(8, Math.min(rect.right - 200, window.innerWidth - 208)), top: rect.bottom + height < window.innerHeight ? rect.bottom + 5 : Math.max(8, rect.top - height - 5) });
    menu.current?.querySelector<HTMLButtonElement>("button")?.focus();
    function dismiss(event: MouseEvent) { if (!menu.current?.contains(event.target as Node) && !button.current?.contains(event.target as Node)) setOpen(false); }
    function key(event: KeyboardEvent) {
      if (event.key === "Escape") { event.preventDefault(); setOpen(false); button.current?.focus(); }
      if (event.key === "Tab") setOpen(false);
      if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
        event.preventDefault();
        const buttons = Array.from(menu.current?.querySelectorAll<HTMLButtonElement>("button") ?? []);
        const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (current + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
        buttons[next]?.focus();
      }
    }
    const close = () => setOpen(false);
    document.addEventListener("mousedown", dismiss);
    document.addEventListener("keydown", key);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => { document.removeEventListener("mousedown", dismiss); document.removeEventListener("keydown", key); window.removeEventListener("resize", close); window.removeEventListener("scroll", close, true); };
  }, [open, actions.length]);
  return <>
    <button ref={button} className="icon-btn" type="button" aria-label={label} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? id : undefined} onClick={() => setOpen(value => !value)}><Icons.more size={16} /></button>
    {open && createPortal(<div id={id} ref={menu} role="menu" aria-label={label} className="row-actions-menu" style={position}>{actions.map(action => <button key={action.label} role="menuitem" type="button" className={action.danger ? "text-danger" : ""} onClick={() => { setOpen(false); button.current?.focus(); Promise.resolve().then(action.run).catch(error => toast.push(error instanceof Error ? error.message : "Не удалось выполнить действие")); }}>{action.label}</button>)}</div>, document.body)}
  </>;
}
