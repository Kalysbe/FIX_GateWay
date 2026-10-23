import { Fragment, useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { csvCell, filterRows, paginateRows, sortRows, type SortState, type SortValue } from "../lib/table";
import { Icons } from "./icons";
import { EmptyState, TableSkeleton } from "./feedback";

export type TableColumn<T> = {
  id: string;
  header: string;
  value: (row: T) => SortValue;
  exportValue?: (row: T) => SortValue;
  cell?: (row: T) => ReactNode;
  numeric?: boolean;
  sortable?: boolean;
  className?: string;
};
export type TableSummary = { label: string; value: ReactNode; tone?: "success" | "danger" | "warn" };

export function DataTable<T>({ rows, columns, rowKey, title, loading = false, searchPlaceholder = "Поиск по таблице…", getSearchText, filters, summary, onRefresh, refreshing, initialSort, emptyTitle = "Записей пока нет", emptyHint, resetKey = "", exportName, exportRows, renderRow, rowClassName, showTotals = true }: {
  rows: T[];
  columns: TableColumn<T>[];
  rowKey: (row: T) => string | number;
  title: string;
  loading?: boolean;
  searchPlaceholder?: string;
  getSearchText?: (row: T) => string;
  filters?: ReactNode;
  summary?: (filtered: T[]) => TableSummary[];
  onRefresh?: () => void;
  refreshing?: boolean;
  initialSort?: SortState;
  emptyTitle?: string;
  emptyHint?: string;
  resetKey?: string;
  exportName?: string;
  exportRows?: (sorted: T[]) => { headers: string[]; rows: SortValue[][] };
  renderRow?: (row: T, index: number) => ReactNode;
  rowClassName?: (row: T) => string;
  showTotals?: boolean;
}) {
  const id = useId();
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortState | undefined>(initialSort);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(20);
  const [comfortable, setComfortable] = useState(false);
  const [hidden, setHidden] = useState<string[]>([]);
  useEffect(() => setPage(0), [resetKey]);
  const visible = columns.filter(c => !hidden.includes(c.id));
  const filtered = useMemo(() => filterRows(rows, query, getSearchText ?? (row => columns.map(c => c.value(row) ?? "").join(" "))), [rows, query, getSearchText, columns]);
  const sorted = useMemo(() => {
    const column = columns.find(c => c.id === sort?.id);
    return sort && column ? sortRows(filtered, column.value, sort.direction) : filtered;
  }, [filtered, columns, sort]);
  const slice = paginateRows(sorted, page, pageSize);
  // Keep a shrinking live dataset on a valid page when rows disappear.
  useEffect(() => { if (page !== slice.page) setPage(slice.page); }, [page, slice.page]);
  const totals = summary?.(filtered) ?? [{ label: "Записей", value: filtered.length }];
  function changeSort(column: TableColumn<T>) {
    setSort(old => ({ id: column.id, direction: old?.id === column.id && old.direction === "asc" ? "desc" : "asc" }));
    setPage(0);
  }
  function exportCsv() {
    const exported = visible.filter(c => c.id !== "actions");
    const data = exportRows?.(sorted) ?? { headers: exported.map(c => c.header), rows: sorted.map(row => exported.map(c => (c.exportValue ?? c.value)(row))) };
    const content = [data.headers.map(csvCell).join(";"), ...data.rows.map(row => row.map(csvCell).join(";"))].join("\r\n");
    const url = URL.createObjectURL(new Blob(["\ufeff", content], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url; link.download = `${exportName}.csv`; link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  return <section className={`data-table ${renderRow ? "data-table-custom" : ""} ${comfortable ? "data-table-comfortable" : ""}`} aria-label={title} aria-busy={loading || refreshing}>
    <div className="table-toolbar">
      <label className="table-search" htmlFor={id}>
        <Icons.search size={16} />
        <input id={id} type="search" aria-label={`Поиск: ${title}`} placeholder={searchPlaceholder} value={query} onChange={e => { setQuery(e.target.value); setPage(0); }} />
      </label>
      {filters && <div className="table-filters">{filters}</div>}
      <div className="table-tools">
        {onRefresh && <button type="button" className="icon-btn" title="Обновить данные" aria-label="Обновить данные" disabled={refreshing} onClick={onRefresh}><Icons.refresh size={16} className={refreshing ? "animate-spin" : ""} /></button>}
        <button type="button" className="icon-btn" title="Высота строк" aria-label="Увеличить высоту строк" aria-pressed={comfortable} onClick={() => setComfortable(v => !v)}><Icons.density size={16} /></button>
        {!renderRow && <details className="table-columns"><summary className="icon-btn" title="Настроить столбцы" aria-label="Настроить столбцы"><Icons.columns size={16} /></summary><div className="table-columns-menu"><strong>Столбцы таблицы</strong>{columns.map(c => <label key={c.id}><input type="checkbox" checked={!hidden.includes(c.id)} disabled={visible.length === 1 && visible[0].id === c.id} onChange={() => setHidden(old => old.includes(c.id) ? old.filter(k => k !== c.id) : [...old, c.id])} />{c.header || "Действия"}</label>)}</div></details>}
        {exportName && <button type="button" className="btn-muted gap-1.5" disabled={!sorted.length || loading} onClick={exportCsv}><Icons.download size={15} />CSV</button>}
      </div>
    </div>
    {loading ? <TableSkeleton cols={Math.min(visible.length, 8)} /> : <div className="table-scroll" tabIndex={0} role="region" aria-label={`Данные: ${title}`}>
      <table className="table"><caption className="sr-only">{title}. Поиск и итоги относятся к загруженным данным.</caption><thead><tr>{visible.map(column => <th key={column.id} className={`${column.numeric ? "numeric" : ""} ${column.className ?? ""}`} aria-sort={sort?.id === column.id ? sort.direction === "asc" ? "ascending" : "descending" : undefined}>
        {column.sortable === false ? column.header : <button type="button" className="table-sort" onClick={() => changeSort(column)}>{column.header}<Icons.sort size={13} className={sort?.id === column.id ? `sort-active ${sort.direction === "desc" ? "rotate-180" : ""}` : ""} /></button>}
      </th>)}</tr></thead><tbody>{slice.rows.map((row, index) => renderRow ? <Fragment key={rowKey(row)}>{renderRow(row, index)}</Fragment> : <tr key={rowKey(row)} className={rowClassName?.(row)}>{visible.map(column => <td key={column.id} className={`${column.numeric ? "numeric" : ""} ${column.className ?? ""}`}>{column.cell ? column.cell(row) : column.value(row) ?? "—"}</td>)}</tr>)}{!slice.rows.length && <tr><td colSpan={visible.length}><EmptyState title={query.trim() ? "Ничего не найдено" : emptyTitle} hint={query.trim() ? "Измените поиск или фильтры" : emptyHint} />{query.trim() && <button type="button" className="btn-muted mx-auto mb-4 flex" onClick={() => { setQuery(""); setPage(0); }}>Сбросить поиск</button>}</td></tr>}</tbody></table>
    </div>}
    {showTotals && <div className="table-totals"><span className="table-totals-label">Итоги по выборке</span>{totals.map(stat => <span className={`table-total ${stat.tone ? `total-${stat.tone}` : ""}`} key={stat.label}>{stat.label}<strong>{stat.value}</strong></span>)}<span className="table-loaded">Загружено: {rows.length.toLocaleString("ru-RU")}</span></div>}
    <div className="table-pagination"><span aria-live="polite">{slice.start}–{slice.end} <span className="text-muted">из {filtered.length.toLocaleString("ru-RU")}</span></span><div className="flex items-center gap-2"><label className="table-page-size">Строк<select value={pageSize} onChange={e => { setPageSize(Number(e.target.value)); setPage(0); }} aria-label="Строк на странице">{[10, 20, 50, 100].map(size => <option key={size}>{size}</option>)}</select></label><button type="button" className="icon-btn" aria-label="Первая страница" disabled={slice.page === 0} onClick={() => setPage(0)}>«</button><button type="button" className="icon-btn" aria-label="Предыдущая страница" disabled={slice.page === 0} onClick={() => setPage(slice.page - 1)}><Icons.chevron size={14} className="rotate-180" /></button><span className="table-page-number">{slice.page + 1} / {slice.pageCount}</span><button type="button" className="icon-btn" aria-label="Следующая страница" disabled={slice.page + 1 >= slice.pageCount} onClick={() => setPage(slice.page + 1)}><Icons.chevron size={14} /></button><button type="button" className="icon-btn" aria-label="Последняя страница" disabled={slice.page + 1 >= slice.pageCount} onClick={() => setPage(slice.pageCount - 1)}>»</button></div></div>
  </section>;
}
