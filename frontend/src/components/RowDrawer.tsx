import { useEffect, useMemo, useState, type ReactNode } from "react";
import { dbValue, type DBOrder } from "../lib/rawOrders";
import { Icons } from "./icons";
import { useToast } from "./Toast";
import { Sheet, SheetClose, SheetContent, SheetTitle } from "./ui/sheet";

/**
 * A database row in a side panel: every column as stored, a filter over the
 * column names and values, and ↑/↓ to step through the rows of the table in
 * the order the user sees them. The page stays where it was underneath.
 */
export function RowDrawer({ row, rows, rowKey, onChange, title, subtitle, actions }: {
  row: DBOrder | null;
  rows: DBOrder[];
  rowKey: (row: DBOrder) => string;
  onChange: (row: DBOrder | null) => void;
  title: (row: DBOrder) => ReactNode;
  subtitle?: (row: DBOrder) => ReactNode;
  actions?: (row: DBOrder) => ReactNode;
}) {
  const toast = useToast();
  const [filter, setFilter] = useState("");
  const index = row ? rows.findIndex(r => rowKey(r) === rowKey(row)) : -1;
  // A live refresh replaces the row object; show its latest values.
  const current = index >= 0 ? rows[index] : row;
  const step = (delta: number) => { const next = rows[index + delta]; if (next) onChange(next); };

  useEffect(() => {
    if (!row) return;
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement)?.closest("input,textarea,select")) return;
      if (e.key === "ArrowDown" || e.key === "j") { e.preventDefault(); step(1); }
      if (e.key === "ArrowUp" || e.key === "k") { e.preventDefault(); step(-1); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const fields = useMemo(() => {
    if (!current) return [];
    const terms = filter.trim().toLocaleLowerCase("ru");
    return Object.entries(current).filter(([k, v]) => !terms || k.toLocaleLowerCase("ru").includes(terms) || dbValue(v).toLocaleLowerCase("ru").includes(terms));
  }, [current, filter]);

  function copy() {
    if (!current) return;
    navigator.clipboard.writeText(JSON.stringify(current, null, 2)).then(
      () => toast.push("Строка скопирована в буфер обмена", "ok"),
      () => toast.push("Браузер не дал скопировать строку"),
    );
  }

  return <Sheet open={!!row} onOpenChange={open => { if (!open) { onChange(null); setFilter(""); } }}>
    <SheetContent side="right" className="w-full max-w-[460px] bg-card text-foreground" aria-describedby={undefined}>
      {current && <>
        <div className="flex items-start justify-between gap-3 border-b border-line px-5 py-4">
          <div className="min-w-0">
            <SheetTitle className="truncate text-base font-semibold">{title(current)}</SheetTitle>
            {subtitle && <div className="mt-0.5 text-xs text-muted">{subtitle(current)}</div>}
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <button type="button" className="icon-btn" aria-label="Предыдущая строка (↑)" title="Предыдущая строка (↑)" disabled={index <= 0} onClick={() => step(-1)}><Icons.chevron size={15} className="-rotate-90" /></button>
            <button type="button" className="icon-btn" aria-label="Следующая строка (↓)" title="Следующая строка (↓)" disabled={index < 0 || index >= rows.length - 1} onClick={() => step(1)}><Icons.chevron size={15} className="rotate-90" /></button>
            <SheetClose className="icon-btn" aria-label="Закрыть"><Icons.x size={16} /></SheetClose>
          </div>
        </div>
        <div className="border-b border-line px-5 py-3">
          <label className="table-search w-full max-w-none">
            <Icons.search size={15} />
            <input type="search" aria-label="Поиск по полям строки" placeholder="Поле или значение…" value={filter} onChange={e => setFilter(e.target.value)} />
          </label>
        </div>
        <dl className="flex-1 overflow-y-auto px-5 py-2 text-[12px]">
          {fields.map(([k, v]) => <div key={k} className="grid grid-cols-[minmax(0,42%)_1fr] gap-3 border-b border-line/60 py-2 last:border-0">
            <dt className="truncate font-mono text-muted" title={k}>{k}</dt>
            <dd className={`break-words font-mono ${v == null || String(v).trim() === "" ? "text-faint" : "text-ink"}`}>{v == null ? "null" : String(v).trim() === "" ? "пусто" : dbValue(v)}</dd>
          </div>)}
          {!fields.length && <p className="py-6 text-center text-muted">Нет полей с «{filter}»</p>}
        </dl>
        <div className="flex flex-wrap items-center gap-2 border-t border-line px-5 py-3">
          {actions?.(current)}
          <button type="button" className="btn-muted ml-auto gap-1.5" onClick={copy}><Icons.copy size={14} />Копировать строку</button>
        </div>
        <p className="px-5 pb-3 text-[11px] text-faint">{index >= 0 ? `Строка ${index + 1} из ${rows.length} · ↑/↓ — соседние строки` : "Строки больше нет в таблице"}</p>
      </>}
    </SheetContent>
  </Sheet>;
}
