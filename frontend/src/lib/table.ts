export type SortValue = string | number | boolean | null | undefined;
export type SortState = { id: string; direction: "asc" | "desc" };

const collator = new Intl.Collator("ru", { numeric: true, sensitivity: "base" });
export const searchText = (value: string) => value.toLocaleLowerCase("ru").replace(/ё/g, "е").trim();

export function filterRows<T>(rows: T[], query: string, text: (row: T) => string): T[] {
  const terms = searchText(query).split(/\s+/).filter(Boolean);
  return terms.length ? rows.filter(row => {
    const haystack = searchText(text(row));
    return terms.every(term => haystack.includes(term));
  }) : rows;
}

export function sortRows<T>(rows: T[], value: (row: T) => SortValue, direction: "asc" | "desc"): T[] {
  const sign = direction === "asc" ? 1 : -1;
  return [...rows].sort((a, b) => {
    const av = value(a), bv = value(b);
    const missing = (v: SortValue) => v == null || v === "" || (typeof v === "number" && !Number.isFinite(v));
    // Missing values always go last, including descending order.
    if (missing(av) || missing(bv)) return missing(av) === missing(bv) ? 0 : missing(av) ? 1 : -1;
    return sign * (typeof av === "number" && typeof bv === "number" ? av - bv : collator.compare(String(av), String(bv)));
  });
}

export function paginateRows<T>(rows: T[], requestedPage: number, pageSize: number) {
  const size = Math.max(1, Math.floor(pageSize) || 1);
  const pageCount = Math.max(1, Math.ceil(rows.length / size));
  const page = Math.max(0, Math.min(Math.floor(requestedPage) || 0, pageCount - 1));
  return { rows: rows.slice(page * size, (page + 1) * size), page, pageCount, start: rows.length ? page * size + 1 : 0, end: Math.min((page + 1) * size, rows.length) };
}

export function csvCell(value: SortValue): string {
  let text = String(value ?? "");
  // Treat spreadsheet formulas in externally supplied labels as plain text.
  if (typeof value === "string" && /^[\s]*[=+\-@]/.test(text)) text = "'" + text;
  return `"${text.replace(/"/g, '""')}"`;
}

export const numberValue = (value: unknown): number | null => value == null || value === "" || !Number.isFinite(Number(value)) ? null : Number(value);
export const dateValue = (value?: string): number | null => value && Number.isFinite(Date.parse(value)) ? Date.parse(value) : null;
export const compactIdentifier = (value: string, max = 28): string => value.length > max ? value.slice(0, max - 11) + "…" + value.slice(-10) : value;
