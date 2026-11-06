import { Fragment, useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { getCoreRowModel, getPaginationRowModel, useReactTable, type ColumnDef, type PaginationState, type Updater, type VisibilityState } from "@tanstack/react-table";
import { csvCell, filterRows, sortRows, type SortState, type SortValue } from "../lib/table";
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
  /** Header tooltip. */
  title?: string;
  /** Starts hidden; the column menu can show it. */
  defaultHidden?: boolean;
};
export type TableSummary = { label: string; value: ReactNode; tone?: "success" | "danger" | "warn" };
/** One page of rows the server returned; the table pages inside it and moves between server pages here. */
export type ServerPage = {
  /** 1-based number of the first and last row of this page in the source table. */
  start: number;
  end: number;
  size: number;
  hasPrevious: boolean;
  hasNext: boolean;
  onPrevious: () => void;
  onNext: () => void;
  busy?: boolean;
};

const PAGE_SIZES = [10, 20, 50, 100];

export function DataTable<T>({ rows, columns, rowKey, title, loading = false, searchPlaceholder = "Поиск по таблице…", getSearchText, filters, summary, onRefresh, refreshing, initialSort, emptyTitle = "Записей пока нет", emptyHint, resetKey = "", exportName, exportRows, renderRow, rowClassName, showTotals = true, serverPage }: {
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
  serverPage?: ServerPage;
}) {
  const id = useId();
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortState | undefined>(initialSort);
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 20 });
  const [comfortable, setComfortable] = useState(false);
  // The user's choices in the column menu, over each column's default.
  const [shown, setShown] = useState<Record<string, boolean>>({});
  const toFirstPage = () => setPagination(p => p.pageIndex === 0 ? p : { ...p, pageIndex: 0 });
  useEffect(toFirstPage, [resetKey]);

  // Search and sort keep the gateway's own rules (Cyrillic ё, numbers, empty values last);
  // TanStack Table handles column visibility and pages.
  const filtered = useMemo(() => filterRows(rows, query, getSearchText ?? (row => columns.map(c => c.value(row) ?? "").join(" "))), [rows, query, getSearchText, columns]);
  const sorted = useMemo(() => {
    const column = columns.find(c => c.id === sort?.id);
    return sort && column ? sortRows(filtered, column.value, sort.direction) : filtered;
  }, [filtered, columns, sort]);
  const byId = useMemo(() => new Map(columns.map(c => [c.id, c])), [columns]);
  const columnDefs = useMemo<ColumnDef<T, SortValue>[]>(() => columns.map(c => ({ id: c.id, header: c.header, accessorFn: c.value })), [columns]);
  const columnVisibility = useMemo<VisibilityState>(() => Object.fromEntries(columns.map(c => [c.id, shown[c.id] ?? !c.defaultHidden])), [columns, shown]);

  // A shrinking live dataset stays on a valid page in the same render.
  const pageCount = Math.max(1, Math.ceil(sorted.length / pagination.pageSize));
  const pageIndex = Math.min(pagination.pageIndex, pageCount - 1);
  useEffect(() => { if (pageIndex !== pagination.pageIndex) setPagination(p => ({ ...p, pageIndex })); }, [pageIndex, pagination.pageIndex]);

  const table = useReactTable<T>({
    data: sorted,
    columns: columnDefs,
    state: { pagination: { pageIndex, pageSize: pagination.pageSize }, columnVisibility },
    onPaginationChange: setPagination,
    onColumnVisibilityChange: (updater: Updater<VisibilityState>) => {
      const next = typeof updater === "function" ? updater(columnVisibility) : updater;
      setShown(old => ({ ...old, ...next }));
    },
    getRowId: row => String(rowKey(row)),
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    autoResetPageIndex: false,
  });

  const visible = table.getVisibleLeafColumns().map(c => byId.get(c.id)).filter((c): c is TableColumn<T> => !!c);
  const pageRows = table.getRowModel().rows;
  const start = sorted.length ? pageIndex * pagination.pageSize + 1 : 0;
  const end = Math.min((pageIndex + 1) * pagination.pageSize, sorted.length);
  const totals = summary?.(filtered) ?? [{ label: "Записей", value: filtered.length }];
  const searching = query.trim() !== "";

  function changeSort(column: TableColumn<T>) {
    setSort(old => ({ id: column.id, direction: old?.id === column.id && old.direction === "asc" ? "desc" : "asc" }));
    toFirstPage();
  }
  function changeQuery(value: string) {
    setQuery(value);
    toFirstPage();
  }
  function exportCsv() {
    const exported = visible.filter(c => c.id !== "actions");
    const data = exportRows?.(sorted) ?? { headers: exported.map(c => c.header), rows: sorted.map(row => exported.map(c => (c.exportValue ?? c.value)(row))) };
    const content = [data.headers.map(csvCell).join(";"), ...data.rows.map(row => row.map(csvCell).join(";"))].join("\r\n");
    const url = URL.createObjectURL(new Blob(["﻿", content], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url; link.download = `${exportName}.csv`; link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  const cellClass = (column: TableColumn<T>) => `${column.numeric ? "numeric" : ""} ${column.className ?? ""}`;

  return <section className={`data-table ${renderRow ? "data-table-custom" : ""} ${comfortable ? "data-table-comfortable" : ""}`} aria-label={title} aria-busy={loading || refreshing}>
    <div className="table-toolbar">
      <label className="table-search" htmlFor={id}>
        <Icons.search size={16} />
        <input id={id} type="search" aria-label={`Поиск: ${title}`} placeholder={searchPlaceholder} value={query} onChange={e => changeQuery(e.target.value)} />
      </label>
      {filters && <div className="table-filters">{filters}</div>}
      <div className="table-tools">
        {onRefresh && <button type="button" className="icon-btn" title="Обновить данные" aria-label="Обновить данные" disabled={refreshing} onClick={onRefresh}><Icons.refresh size={16} className={refreshing ? "animate-spin" : ""} /></button>}
        <button type="button" className="icon-btn" title="Высота строк" aria-label="Увеличить высоту строк" aria-pressed={comfortable} onClick={() => setComfortable(v => !v)}><Icons.density size={16} /></button>
        {!renderRow && <details className="table-columns"><summary className="icon-btn" title="Настроить столбцы" aria-label="Настроить столбцы"><Icons.columns size={16} /></summary><div className="table-columns-menu"><strong>Столбцы таблицы</strong>{table.getAllLeafColumns().map(col => {
          const c = byId.get(col.id)!;
          const last = visible.length === 1 && visible[0].id === c.id;
          return <label key={c.id} title={c.title}><input type="checkbox" checked={col.getIsVisible()} disabled={last} onChange={() => col.toggleVisibility()} />{c.header || "Действия"}</label>;
        })}</div></details>}
        {exportName && <button type="button" className="btn-muted gap-1.5" disabled={!sorted.length || loading} onClick={exportCsv}><Icons.download size={15} />CSV</button>}
      </div>
    </div>
    {loading ? <TableSkeleton cols={Math.min(visible.length, 8)} /> : <div className="table-scroll" tabIndex={0} role="region" aria-label={`Данные: ${title}`}>
      <table className="table"><caption className="sr-only">{title}. Поиск и итоги относятся к загруженным данным.</caption>
        <thead><tr>{visible.map(column => <th key={column.id} className={cellClass(column)} title={column.title} aria-sort={sort?.id === column.id ? sort.direction === "asc" ? "ascending" : "descending" : undefined}>
          {column.sortable === false ? column.header : <button type="button" className="table-sort" onClick={() => changeSort(column)}>{column.header}<Icons.sort size={13} className={sort?.id === column.id ? `sort-active ${sort.direction === "desc" ? "rotate-180" : ""}` : ""} /></button>}
        </th>)}</tr></thead>
        <tbody>
          {pageRows.map((row, index) => renderRow
            ? <Fragment key={row.id}>{renderRow(row.original, index)}</Fragment>
            : <tr key={row.id} className={rowClassName?.(row.original)}>{visible.map(column => <td key={column.id} className={cellClass(column)}>{column.cell ? column.cell(row.original) : column.value(row.original) ?? "—"}</td>)}</tr>)}
          {!pageRows.length && <tr><td colSpan={visible.length}>
            <EmptyState title={searching ? "Ничего не найдено" : emptyTitle} hint={searching ? "Измените поиск или фильтры" : emptyHint} />
            {searching && <button type="button" className="btn-muted mx-auto mb-4 flex" onClick={() => changeQuery("")}>Сбросить поиск</button>}
          </td></tr>}
        </tbody>
      </table>
    </div>}
    {showTotals && <div className="table-totals"><span className="table-totals-label">Итоги по выборке</span>{totals.map(stat => <span className={`table-total ${stat.tone ? `total-${stat.tone}` : ""}`} key={stat.label}>{stat.label}<strong>{stat.value}</strong></span>)}<span className="table-loaded">Загружено: {rows.length.toLocaleString("ru-RU")}</span></div>}
    <div className="table-pagination">
      <span aria-live="polite">{start}–{end} <span className="text-muted">из {filtered.length.toLocaleString("ru-RU")}</span></span>
      {serverPage && <div className="table-server-page" role="group" aria-label="Страницы источника">
        <span aria-live="polite">Строки БД {serverPage.end >= serverPage.start ? `${serverPage.start}–${serverPage.end}` : "—"}</span>
        <button type="button" className="btn-muted" disabled={!serverPage.hasPrevious || serverPage.busy} onClick={serverPage.onPrevious}><Icons.chevron size={14} className="rotate-180" />Предыдущие {serverPage.size}</button>
        <button type="button" className="btn-muted" disabled={!serverPage.hasNext || serverPage.busy} onClick={serverPage.onNext}>Следующие {serverPage.size}<Icons.chevron size={14} /></button>
      </div>}
      <div className="flex items-center gap-2">
        <label className="table-page-size">Строк<select value={pagination.pageSize} onChange={e => setPagination({ pageIndex: 0, pageSize: Number(e.target.value) })} aria-label="Строк на странице">{PAGE_SIZES.map(size => <option key={size}>{size}</option>)}</select></label>
        <button type="button" className="icon-btn" aria-label="Первая страница" disabled={!table.getCanPreviousPage()} onClick={() => table.firstPage()}><Icons.chevronsLeft size={14} /></button>
        <button type="button" className="icon-btn" aria-label="Предыдущая страница" disabled={!table.getCanPreviousPage()} onClick={() => table.previousPage()}><Icons.chevron size={14} className="rotate-180" /></button>
        <span className="table-page-number">{pageIndex + 1} / {pageCount}</span>
        <button type="button" className="icon-btn" aria-label="Следующая страница" disabled={!table.getCanNextPage()} onClick={() => table.nextPage()}><Icons.chevron size={14} /></button>
        <button type="button" className="icon-btn" aria-label="Последняя страница" disabled={!table.getCanNextPage()} onClick={() => table.lastPage()}><Icons.chevronsRight size={14} /></button>
      </div>
    </div>
  </section>;
}
