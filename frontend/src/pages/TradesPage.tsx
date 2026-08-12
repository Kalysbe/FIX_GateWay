import { useQuery } from "@tanstack/react-query";
import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  type ColumnDef,
  type ColumnFiltersState,
  type FilterFn,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, sideLabel } from "../labels";

type Trade = {
  id: string;
  order_id: string;
  exec_id: string;
  last_qty: string | number;
  last_px: string | number;
  cum_qty: string | number;
  leaves_qty: string | number;
  adapter_id?: string | null;
  created_at: string;
  client_order_id?: string;
  symbol?: string;
  board?: string;
  side?: string;
  client_code?: string;
  quik_firm?: string;
  firm_name?: string;
  fix_cl_ord_id?: string | null;
  quik_order_id?: string | null;
};

const globalFilterFn: FilterFn<Trade> = (row, _columnId, filterValue) => {
  const q = String(filterValue || "").trim().toLowerCase();
  if (!q) return true;
  const t = row.original;
  const hay = [
    t.exec_id,
    t.client_order_id,
    t.order_id,
    t.firm_name,
    t.quik_firm,
    t.client_code,
    t.symbol,
    t.board,
    t.side,
    t.adapter_id,
    t.fix_cl_ord_id,
    t.quik_order_id,
    String(t.last_qty),
    String(t.last_px),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return hay.includes(q);
};

function num(v: string | number | null | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function SideBadge({ side }: { side?: string }) {
  const buy = (side || "").toUpperCase() === "BUY";
  return (
    <span
      className={`inline-flex rounded-md px-2 py-0.5 text-xs font-semibold ${
        buy ? "bg-emerald-50 text-emerald-700" : "bg-rose-50 text-rose-700"
      }`}
    >
      {sideLabel(side)}
    </span>
  );
}

export default function TradesPage() {
  const q = useQuery({
    queryKey: ["admin-trades"],
    queryFn: () => api<{ trades: Trade[] }>("/api/v1/admin/trades"),
    refetchInterval: 5000,
  });

  const data = q.data?.trades || [];
  const [sorting, setSorting] = useState<SortingState>([{ id: "created_at", desc: true }]);
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
  const [globalFilter, setGlobalFilter] = useState("");
  const [pageIndex, setPageIndex] = useState(0);
  const [pageSize, setPageSize] = useState(25);

  const firmOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      const label = t.firm_name || t.quik_firm;
      if (label) set.add(label);
    });
    return Array.from(set).sort((a, b) => a.localeCompare(b, "ru"));
  }, [data]);

  const symbolOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      if (t.symbol) set.add(t.symbol);
    });
    return Array.from(set).sort();
  }, [data]);

  const adapterOptions = useMemo(() => {
    const set = new Set<string>();
    data.forEach((t) => {
      if (t.adapter_id) set.add(t.adapter_id);
    });
    return Array.from(set).sort();
  }, [data]);

  const columns = useMemo<ColumnDef<Trade>[]>(
    () => [
      {
        id: "created_at",
        accessorFn: (r) => new Date(r.created_at).getTime(),
        header: "Время",
        cell: ({ row }) => (
          <span className="whitespace-nowrap font-mono text-[11px] text-slate-600">
            {formatDateTime(row.original.created_at)}
          </span>
        ),
      },
      {
        accessorKey: "exec_id",
        header: "ExecID",
        cell: ({ getValue }) => (
          <span className="block max-w-[160px] truncate font-mono text-[11px]" title={String(getValue() || "")}>
            {String(getValue() || "—")}
          </span>
        ),
      },
      {
        accessorKey: "client_order_id",
        header: "Ордер",
        cell: ({ row }) => (
          <Link
            className="font-mono text-xs text-accent hover:underline"
            to={`/orders/${row.original.order_id}`}
            title={row.original.client_order_id || row.original.order_id}
          >
            {row.original.client_order_id || String(row.original.order_id).slice(0, 8)}
          </Link>
        ),
      },
      {
        id: "firm",
        accessorFn: (r) => r.firm_name || r.quik_firm || "",
        header: "Фирма",
        cell: ({ row }) => (
          <div>
            <div className="text-sm">{row.original.firm_name || "—"}</div>
            {row.original.quik_firm ? (
              <div className="font-mono text-[11px] text-slate-500">{row.original.quik_firm}</div>
            ) : null}
          </div>
        ),
        filterFn: (row, _id, value) => {
          if (!value) return true;
          const label = row.original.firm_name || row.original.quik_firm || "";
          return label === value;
        },
      },
      {
        accessorKey: "client_code",
        header: "Клиент",
        cell: ({ getValue }) => <span className="font-mono text-xs">{String(getValue() || "—")}</span>,
      },
      {
        accessorKey: "symbol",
        header: "Инструмент",
        cell: ({ row }) => (
          <div>
            <div className="font-mono text-xs">{row.original.symbol || "—"}</div>
            {row.original.board ? <div className="text-[11px] text-slate-500">{row.original.board}</div> : null}
          </div>
        ),
        filterFn: (row, _id, value) => !value || row.original.symbol === value,
      },
      {
        accessorKey: "side",
        header: "Сторона",
        cell: ({ row }) => <SideBadge side={row.original.side} />,
        filterFn: (row, _id, value) => !value || row.original.side === value,
      },
      {
        id: "last_qty",
        accessorFn: (r) => num(r.last_qty),
        header: "Кол-во",
        cell: ({ row }) => <span className="font-mono text-sm tabular-nums">{row.original.last_qty}</span>,
      },
      {
        id: "last_px",
        accessorFn: (r) => num(r.last_px),
        header: "Цена",
        cell: ({ row }) => <span className="font-mono text-sm tabular-nums">{row.original.last_px}</span>,
      },
      {
        id: "cum_qty",
        accessorFn: (r) => num(r.cum_qty),
        header: "Накоплено",
        cell: ({ row }) => (
          <span className="font-mono text-xs tabular-nums text-slate-600">{row.original.cum_qty}</span>
        ),
      },
      {
        id: "leaves_qty",
        accessorFn: (r) => num(r.leaves_qty),
        header: "Остаток",
        cell: ({ row }) => (
          <span className="font-mono text-xs tabular-nums text-slate-600">{row.original.leaves_qty}</span>
        ),
      },
      {
        accessorKey: "adapter_id",
        header: "Адаптер",
        cell: ({ getValue }) => (
          <span className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[11px] text-slate-600">
            {String(getValue() || "—")}
          </span>
        ),
        filterFn: (row, _id, value) => !value || row.original.adapter_id === value,
      },
    ],
    [],
  );

  const table = useReactTable({
    data,
    columns,
    state: {
      sorting,
      columnFilters,
      globalFilter,
      pagination: { pageIndex, pageSize },
    },
    onSortingChange: setSorting,
    onColumnFiltersChange: (updater) => {
      setColumnFilters(updater);
      setPageIndex(0);
    },
    onGlobalFilterChange: (v) => {
      setGlobalFilter(String(v ?? ""));
      setPageIndex(0);
    },
    onPaginationChange: (updater) => {
      const next = typeof updater === "function" ? updater({ pageIndex, pageSize }) : updater;
      setPageIndex(next.pageIndex);
      setPageSize(next.pageSize);
    },
    globalFilterFn,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  });

  const filtered = table.getFilteredRowModel().rows.length;
  const total = data.length;
  const pageCount = Math.max(1, table.getPageCount());

  const sideFilter = (columnFilters.find((f) => f.id === "side")?.value as string) || "";
  const firmFilter = (columnFilters.find((f) => f.id === "firm")?.value as string) || "";
  const symbolFilter = (columnFilters.find((f) => f.id === "symbol")?.value as string) || "";
  const adapterFilter = (columnFilters.find((f) => f.id === "adapter_id")?.value as string) || "";

  function setFilter(id: string, value: string) {
    setColumnFilters((prev) => {
      const rest = prev.filter((f) => f.id !== id);
      return value ? [...rest, { id, value }] : rest;
    });
    setPageIndex(0);
  }

  function exportCsv() {
    const rows = table.getFilteredRowModel().rows.map((r) => r.original);
    const header = [
      "created_at",
      "exec_id",
      "client_order_id",
      "firm_name",
      "quik_firm",
      "client_code",
      "symbol",
      "board",
      "side",
      "last_qty",
      "last_px",
      "cum_qty",
      "leaves_qty",
      "adapter_id",
    ];
    const lines = [
      header.join(","),
      ...rows.map((t) =>
        header
          .map((k) => {
            const v = String((t as Record<string, unknown>)[k] ?? "");
            return `"${v.replace(/"/g, '""')}"`;
          })
          .join(","),
      ),
    ];
    const blob = new Blob([lines.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `trades-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  const buyCount = useMemo(
    () => data.filter((t) => (t.side || "").toUpperCase() === "BUY").length,
    [data],
  );
  const sellCount = total - buyCount;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Сделки</h1>
          <p className="text-sm text-slate-500">
            FIX ExecutionReport · LastQty &gt; 0 · обновление каждые 5 с
          </p>
        </div>
        <div className="flex flex-wrap gap-2 text-xs">
          <StatPill label="Всего" value={total} />
          <StatPill label="Покупка" value={buyCount} tone="buy" />
          <StatPill label="Продажа" value={sellCount} tone="sell" />
          <StatPill label="После фильтра" value={filtered} />
        </div>
      </div>

      <div className="card space-y-3 bg-white">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[240px] flex-1">
            <input
              className="input pl-9"
              placeholder="Поиск: ExecID, ордер, фирма, клиент, инструмент…"
              value={globalFilter}
              onChange={(e) => {
                setGlobalFilter(e.target.value);
                setPageIndex(0);
              }}
            />
            <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400">⌕</span>
          </div>
          <select className="input w-auto" value={sideFilter} onChange={(e) => setFilter("side", e.target.value)}>
            <option value="">Сторона: все</option>
            <option value="BUY">Покупка</option>
            <option value="SELL">Продажа</option>
          </select>
          <select className="input w-auto max-w-[180px]" value={firmFilter} onChange={(e) => setFilter("firm", e.target.value)}>
            <option value="">Фирма: все</option>
            {firmOptions.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
          <select
            className="input w-auto max-w-[180px]"
            value={symbolFilter}
            onChange={(e) => setFilter("symbol", e.target.value)}
          >
            <option value="">Инструмент: все</option>
            {symbolOptions.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <select
            className="input w-auto max-w-[160px]"
            value={adapterFilter}
            onChange={(e) => setFilter("adapter_id", e.target.value)}
          >
            <option value="">Адаптер: все</option>
            {adapterOptions.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
          <button
            className="btn-muted"
            onClick={() => {
              setGlobalFilter("");
              setColumnFilters([]);
              setPageIndex(0);
            }}
          >
            Сбросить
          </button>
          <button className="btn-muted" onClick={() => void q.refetch()}>
            Обновить
          </button>
          <button className="btn" onClick={exportCsv} disabled={filtered === 0}>
            CSV
          </button>
        </div>

        <div className="overflow-x-auto rounded-lg border border-slate-200">
          <table className="w-full min-w-[1100px] text-left text-sm">
            <thead className="sticky top-0 z-10 bg-slate-50">
              {table.getHeaderGroups().map((hg) => (
                <tr key={hg.id} className="border-b border-slate-200">
                  {hg.headers.map((h) => {
                    const sorted = h.column.getIsSorted();
                    return (
                      <th
                        key={h.id}
                        className="select-none px-3 py-2.5 text-xs font-semibold uppercase tracking-wide text-slate-500"
                      >
                        {h.isPlaceholder ? null : (
                          <button
                            type="button"
                            className={`inline-flex items-center gap-1 ${
                              h.column.getCanSort() ? "hover:text-accent" : ""
                            }`}
                            onClick={h.column.getToggleSortingHandler()}
                          >
                            {flexRender(h.column.columnDef.header, h.getContext())}
                            <span className="font-mono text-[10px] text-slate-400">
                              {sorted === "asc" ? "↑" : sorted === "desc" ? "↓" : "↕"}
                            </span>
                          </button>
                        )}
                      </th>
                    );
                  })}
                </tr>
              ))}
            </thead>
            <tbody>
              {q.isLoading && (
                <tr>
                  <td colSpan={columns.length} className="px-3 py-10 text-center text-slate-500">
                    Загрузка сделок…
                  </td>
                </tr>
              )}
              {!q.isLoading && table.getRowModel().rows.length === 0 && (
                <tr>
                  <td colSpan={columns.length} className="px-3 py-10 text-center text-slate-500">
                    {total === 0
                      ? "Нет сделок — появятся после FILL/PARTIAL из FIX"
                      : "Ничего не найдено по текущим фильтрам"}
                  </td>
                </tr>
              )}
              {table.getRowModel().rows.map((row) => (
                <tr key={row.id} className="border-b border-slate-100 transition-colors hover:bg-[#f0fafb]">
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id} className="px-3 py-2 align-middle">
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
          <div className="text-slate-500">
            Показано{" "}
            <span className="font-mono text-ink">
              {filtered === 0 ? 0 : pageIndex * pageSize + 1}–{Math.min((pageIndex + 1) * pageSize, filtered)}
            </span>{" "}
            из <span className="font-mono text-ink">{filtered}</span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2 text-slate-500">
              На странице
              <select
                className="input w-auto py-1"
                value={pageSize}
                onChange={(e) => {
                  setPageSize(Number(e.target.value));
                  setPageIndex(0);
                }}
              >
                {[10, 25, 50, 100].map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="btn-muted px-2 py-1"
              disabled={!table.getCanPreviousPage()}
              onClick={() => table.setPageIndex(0)}
            >
              «
            </button>
            <button
              className="btn-muted px-2 py-1"
              disabled={!table.getCanPreviousPage()}
              onClick={() => table.previousPage()}
            >
              ‹
            </button>
            <span className="min-w-[7rem] text-center font-mono text-xs text-slate-600">
              {pageIndex + 1} / {pageCount}
            </span>
            <button
              className="btn-muted px-2 py-1"
              disabled={!table.getCanNextPage()}
              onClick={() => table.nextPage()}
            >
              ›
            </button>
            <button
              className="btn-muted px-2 py-1"
              disabled={!table.getCanNextPage()}
              onClick={() => table.setPageIndex(pageCount - 1)}
            >
              »
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function StatPill({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: "buy" | "sell";
}) {
  const color =
    tone === "buy"
      ? "border-emerald-200 bg-emerald-50 text-emerald-800"
      : tone === "sell"
        ? "border-rose-200 bg-rose-50 text-rose-800"
        : "border-slate-200 bg-white text-ink";
  return (
    <div className={`rounded-lg border px-3 py-1.5 ${color}`}>
      <span className="mr-2 text-slate-500">{label}</span>
      <span className="font-mono font-semibold">{value}</span>
    </div>
  );
}
