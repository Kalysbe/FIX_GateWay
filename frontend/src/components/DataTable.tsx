import { Fragment, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { getCoreRowModel, getPaginationRowModel, useReactTable, type ColumnDef, type PaginationState, type RowSelectionState, type Updater, type VisibilityState } from "@tanstack/react-table";
import { csvCell, filterRows, sortRows, type SortState, type SortValue } from "../lib/table";
import { cellKey, useLiveMarks } from "../lib/liveRows";
import { Icons } from "./icons";
import { EmptyState, TableSkeleton } from "./feedback";
import { useConfirm, type ConfirmOptions } from "./confirm";
import { useToast } from "./Toast";

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
  /** In a live table, a change of this value flashes green (up) or red (down). */
  trend?: boolean;
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
/** An action on the selected rows. */
export type BulkAction<T> = {
  label: string;
  run: (rows: T[]) => Promise<unknown>;
  danger?: boolean;
  confirm?: (rows: T[]) => Omit<ConfirmOptions, "action" | "success">;
  success?: (rows: T[]) => string;
};

const PAGE_SIZES = [10, 20, 50, 100];
const INTERACTIVE = "a,button,input,select,textarea,label,summary,[role=menuitem],[data-row-ignore]";

export function DataTable<T>({ rows, columns, rowKey, title, loading = false, searchPlaceholder = "Поиск по таблице…", getSearchText, filters, summary, onRefresh, refreshing, initialSort, emptyTitle = "Записей пока нет", emptyHint, resetKey = "", exportName, exportRows, renderRow, rowClassName, showTotals = true, serverPage, live = false, onRowClick, activeRowKey, urlState, selection }: {
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
  /** Highlights rows that appeared or changed since the previous refresh. */
  live?: boolean;
  /** Clicking a row (or Enter on it) opens it; `sorted` is the order the user sees. */
  onRowClick?: (row: T, sorted: T[]) => void;
  activeRowKey?: string | number | null;
  /** Keeps search, sort and page in the URL; the string prefixes the parameter names. */
  urlState?: string | boolean;
  /** Checkboxes and actions on the chosen rows. */
  selection?: { actions?: BulkAction<T>[]; canSelect?: (row: T) => boolean };
}) {
  const id = useId();
  const toast = useToast();
  const ask = useConfirm();
  const [params, setParams] = useSearchParams();
  const prefix = urlState === true ? "" : urlState || "";
  const inUrl = urlState !== undefined && urlState !== false;
  const name = (key: string) => prefix + key;

  const [localQuery, setLocalQuery] = useState("");
  const [localSort, setLocalSort] = useState<SortState | undefined>(initialSort);
  const [localPagination, setLocalPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 20 });
  const urlSort = (() => {
    const [sid, dir] = (params.get(name("sort")) ?? "").split(":");
    return sid && columns.some(c => c.id === sid) ? { id: sid, direction: dir === "desc" ? "desc" : "asc" } as SortState : initialSort;
  })();
  const query = inUrl ? params.get(name("q")) ?? "" : localQuery;
  const sort = inUrl ? urlSort : localSort;
  const pagination: PaginationState = inUrl
    ? { pageIndex: Math.max(0, (Number(params.get(name("page"))) || 1) - 1), pageSize: PAGE_SIZES.includes(Number(params.get(name("rows")))) ? Number(params.get(name("rows"))) : 20 }
    : localPagination;

  function writeUrl(patch: Record<string, string | null>) {
    setParams(prev => {
      const out = new URLSearchParams(prev);
      for (const [k, v] of Object.entries(patch)) { if (v == null || v === "") out.delete(name(k)); else out.set(name(k), v); }
      return out;
    }, { replace: true });
  }
  const setQuery = (value: string) => inUrl ? writeUrl({ q: value, page: null }) : (setLocalQuery(value), setLocalPagination(p => ({ ...p, pageIndex: 0 })));
  const setSort = (next: SortState) => inUrl ? writeUrl({ sort: `${next.id}:${next.direction}`, page: null }) : (setLocalSort(next), setLocalPagination(p => ({ ...p, pageIndex: 0 })));
  const setPagination = (updater: Updater<PaginationState>) => {
    const next = typeof updater === "function" ? updater(pagination) : updater;
    if (inUrl) writeUrl({ page: next.pageIndex ? String(next.pageIndex + 1) : null, rows: next.pageSize === 20 ? null : String(next.pageSize) });
    else setLocalPagination(next);
  };
  const toFirstPage = () => { if (pagination.pageIndex !== 0) setPagination(p => ({ ...p, pageIndex: 0 })); };
  const firstReset = useRef(true);
  useEffect(() => {
    // The URL page survives a reload; only a later filter change goes back to page 1.
    if (firstReset.current) { firstReset.current = false; return; }
    toFirstPage();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resetKey]);

  const [comfortable, setComfortable] = useState(false);
  // The user's choices in the column menu, over each column's default.
  const [shown, setShown] = useState<Record<string, boolean>>({});
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const [bulkBusy, setBulkBusy] = useState(false);

  // Search and sort keep the gateway's own rules (Cyrillic ё, numbers, empty values last);
  // TanStack Table handles column visibility, selection and pages.
  const filtered = useMemo(() => filterRows(rows, query, getSearchText ?? (row => columns.map(c => c.value(row) ?? "").join(" "))), [rows, query, getSearchText, columns]);
  const sorted = useMemo(() => {
    const column = columns.find(c => c.id === sort?.id);
    return sort && column ? sortRows(filtered, column.value, sort.direction) : filtered;
  }, [filtered, columns, sort]);
  const byId = useMemo(() => new Map(columns.map(c => [c.id, c])), [columns]);
  const columnDefs = useMemo<ColumnDef<T, SortValue>[]>(() => columns.map(c => ({ id: c.id, header: c.header, accessorFn: c.value })), [columns]);
  const columnVisibility = useMemo<VisibilityState>(() => Object.fromEntries(columns.map(c => [c.id, shown[c.id] ?? !c.defaultHidden])), [columns, shown]);
  const marks = useLiveMarks({ rows, rowKey, enabled: live, resetKey, trend: columns.filter(c => c.trend) });

  // A shrinking live dataset stays on a valid page in the same render.
  const pageCount = Math.max(1, Math.ceil(sorted.length / pagination.pageSize));
  const pageIndex = Math.min(pagination.pageIndex, pageCount - 1);
  // While rows load, a page from the URL is kept rather than reset to the first.
  useEffect(() => { if (!loading && sorted.length && pageIndex !== pagination.pageIndex) setPagination(p => ({ ...p, pageIndex })); });

  const table = useReactTable<T>({
    data: sorted,
    columns: columnDefs,
    state: { pagination: { pageIndex, pageSize: pagination.pageSize }, columnVisibility, rowSelection },
    onPaginationChange: setPagination,
    onColumnVisibilityChange: (updater: Updater<VisibilityState>) => {
      const next = typeof updater === "function" ? updater(columnVisibility) : updater;
      setShown(old => ({ ...old, ...next }));
    },
    enableRowSelection: selection ? (row => selection.canSelect?.(row.original) ?? true) : false,
    onRowSelectionChange: setRowSelection,
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
  // Only rows still loaded count: a row that left the live table is no longer selected.
  const selected = useMemo(() => selection ? rows.filter(r => rowSelection[String(rowKey(r))]) : [], [rows, rowSelection, selection, rowKey]);
  const clearSelection = () => setRowSelection({});
  const pageSelectable = pageRows.filter(r => r.getCanSelect());
  const allPageSelected = pageSelectable.length > 0 && pageSelectable.every(r => r.getIsSelected());
  const somePageSelected = pageSelectable.some(r => r.getIsSelected());
  const headerBox = useRef<HTMLInputElement>(null);
  useEffect(() => { if (headerBox.current) headerBox.current.indeterminate = somePageSelected && !allPageSelected; });

  function changeSort(column: TableColumn<T>) {
    setSort({ id: column.id, direction: sort?.id === column.id && sort.direction === "asc" ? "desc" : "asc" });
  }
  function download(list: T[], suffix = "") {
    const exported = visible.filter(c => c.id !== "actions");
    const data = exportRows?.(list) ?? { headers: exported.map(c => c.header), rows: list.map(row => exported.map(c => (c.exportValue ?? c.value)(row))) };
    const content = [data.headers.map(csvCell).join(";"), ...data.rows.map(row => row.map(csvCell).join(";"))].join("\r\n");
    const url = URL.createObjectURL(new Blob(["﻿", content], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url; link.download = `${exportName}${suffix}.csv`; link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  async function runBulk(action: BulkAction<T>) {
    const list = selected;
    if (!list.length) return;
    if (action.confirm) {
      const ok = await ask({ ...action.confirm(list), action: () => action.run(list), success: action.success?.(list) });
      if (ok) clearSelection();
      return;
    }
    setBulkBusy(true);
    try {
      await action.run(list);
      if (action.success) toast.push(action.success(list), "ok");
      clearSelection();
    } catch (error) {
      toast.push(error instanceof Error && error.message ? error.message : "Не удалось выполнить действие");
    } finally {
      setBulkBusy(false);
    }
  }
  function rowEvents(row: T, key: string) {
    if (!onRowClick) return {};
    const open = () => onRowClick(row, sorted);
    return {
      tabIndex: 0,
      "aria-selected": activeRowKey != null && String(activeRowKey) === key ? true : undefined,
      onClick: (e: MouseEvent<HTMLTableRowElement>) => { if (!(e.target as HTMLElement).closest(INTERACTIVE) && !window.getSelection()?.toString()) open(); },
      onKeyDown: (e: KeyboardEvent<HTMLTableRowElement>) => { if ((e.key === "Enter" || e.key === " ") && e.target === e.currentTarget) { e.preventDefault(); open(); } },
    };
  }
  const cellClass = (column: TableColumn<T>) => `${column.numeric ? "numeric" : ""} ${column.className ?? ""}`;
  const colSpan = visible.length + (selection ? 1 : 0);

  return <section className={`data-table ${renderRow ? "data-table-custom" : ""} ${comfortable ? "data-table-comfortable" : ""} ${onRowClick ? "data-table-clickable" : ""}`} aria-label={title} aria-busy={loading || refreshing}>
    <div className="table-toolbar">
      <label className="table-search" htmlFor={id}>
        <Icons.search size={16} />
        <input id={id} type="search" aria-label={`Поиск: ${title}`} placeholder={searchPlaceholder} value={query} onChange={e => setQuery(e.target.value)} />
      </label>
      {filters && <div className="table-filters">{filters}</div>}
      <div className="table-tools">
        {live && marks.added > 0 && <span className="live-badge" role="status">+{marks.added} {marks.added === 1 ? "новая строка" : "новых строк"}</span>}
        {onRefresh && <button type="button" className="icon-btn" title="Обновить данные" aria-label="Обновить данные" disabled={refreshing} onClick={onRefresh}><Icons.refresh size={16} className={refreshing ? "animate-spin" : ""} /></button>}
        <button type="button" className="icon-btn" title="Высота строк" aria-label="Увеличить высоту строк" aria-pressed={comfortable} onClick={() => setComfortable(v => !v)}><Icons.density size={16} /></button>
        {!renderRow && <details className="table-columns"><summary className="icon-btn" title="Настроить столбцы" aria-label="Настроить столбцы"><Icons.columns size={16} /></summary><div className="table-columns-menu"><strong>Столбцы таблицы</strong>{table.getAllLeafColumns().map(col => {
          const c = byId.get(col.id)!;
          const last = visible.length === 1 && visible[0].id === c.id;
          return <label key={c.id} title={c.title}><input type="checkbox" checked={col.getIsVisible()} disabled={last} onChange={() => col.toggleVisibility()} />{c.header || "Действия"}</label>;
        })}</div></details>}
        {exportName && <button type="button" className="btn-muted gap-1.5" disabled={!sorted.length || loading} onClick={() => download(sorted)}><Icons.download size={15} />CSV</button>}
      </div>
    </div>
    {selection && selected.length > 0 && <div className="table-bulk" role="region" aria-label="Действия с выбранными строками">
      <strong>Выбрано: {selected.length.toLocaleString("ru-RU")}</strong>
      {exportName && <button type="button" className="btn-muted gap-1.5" onClick={() => download(selected, "-selected")}><Icons.download size={14} />CSV выбранных</button>}
      {selection.actions?.map(action => <button key={action.label} type="button" className={action.danger ? "btn-muted text-danger" : "btn-muted"} disabled={bulkBusy} onClick={() => void runBulk(action)}>{bulkBusy && <Icons.refresh size={13} className="animate-spin" />}{action.label}</button>)}
      <button type="button" className="btn-muted ml-auto" onClick={clearSelection}>Снять выбор</button>
    </div>}
    {loading ? <TableSkeleton cols={Math.min(visible.length, 8)} /> : <div className="table-scroll" tabIndex={0} role="region" aria-label={`Данные: ${title}`}>
      <table className="table"><caption className="sr-only">{title}. Поиск и итоги относятся к загруженным данным.</caption>
        <thead><tr>
          {selection && <th className="table-select"><input ref={headerBox} type="checkbox" aria-label="Выбрать строки страницы" checked={allPageSelected} disabled={!pageSelectable.length} onChange={() => table.toggleAllPageRowsSelected(!allPageSelected)} /></th>}
          {visible.map(column => <th key={column.id} className={cellClass(column)} title={column.title} aria-sort={sort?.id === column.id ? sort.direction === "asc" ? "ascending" : "descending" : undefined}>
            {column.sortable === false ? column.header : <button type="button" className="table-sort" onClick={() => changeSort(column)}>{column.header}<Icons.sort size={13} className={sort?.id === column.id ? `sort-active ${sort.direction === "desc" ? "rotate-180" : ""}` : ""} /></button>}
          </th>)}
        </tr></thead>
        <tbody>
          {pageRows.map((row, index) => {
            const mark = marks.rows.get(row.id);
            const markClass = mark ? `row-flash-${mark}` : "";
            if (renderRow) return <Fragment key={row.id}>{renderRow(row.original, index)}</Fragment>;
            return <tr key={row.id} className={`${rowClassName?.(row.original) ?? ""} ${markClass} ${row.getIsSelected() ? "row-selected" : ""}`} {...rowEvents(row.original, row.id)}>
              {selection && <td className="table-select"><input type="checkbox" aria-label="Выбрать строку" checked={row.getIsSelected()} disabled={!row.getCanSelect()} onChange={row.getToggleSelectedHandler()} /></td>}
              {visible.map(column => {
                const trend = marks.cells.get(cellKey(row.id, column.id));
                return <td key={column.id} className={`${cellClass(column)} ${trend ? `cell-${trend}` : ""}`}>{column.cell ? column.cell(row.original) : column.value(row.original) ?? "—"}</td>;
              })}
            </tr>;
          })}
          {!pageRows.length && <tr><td colSpan={colSpan}>
            <EmptyState title={searching ? "Ничего не найдено" : emptyTitle} hint={searching ? "Измените поиск или фильтры" : emptyHint} />
            {searching && <button type="button" className="btn-muted mx-auto mb-4 flex" onClick={() => setQuery("")}>Сбросить поиск</button>}
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
