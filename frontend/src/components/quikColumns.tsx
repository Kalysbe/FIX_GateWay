import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { TableColumn } from "./DataTable";
import { dbSortValue, dbValue, type DBOrder } from "../lib/rawOrders";
import { formatDate, formatDateTime, formatNumber, formatTime, sideClass, stateTone } from "../lib/quikFormat";

export type QuikKind = "text" | "number" | "date" | "datetime" | "time" | "side" | "state";

// A chosen column of a QUIK table. Fields are database column names, matched
// case-insensitively (QUIK spells "Accruedint" and "AccruedInt"); the first
// one present in the rows is used, a column whose fields are all absent is
// left out.
export type QuikColumn = {
  id: string;
  header: string;
  field: string | string[];
  kind?: QuikKind;
  /** Minimum decimals of a number. */
  frac?: number;
  /** Second, muted line under the value. */
  sub?: string | string[];
  subKind?: QuikKind;
  /** Hidden until enabled in the column menu. */
  hidden?: boolean;
  link?: (row: DBOrder) => string | undefined;
  render?: (row: DBOrder, text: ReactNode) => ReactNode;
};

function fieldIndex(rows: DBOrder[]): Map<string, string> {
  const index = new Map<string, string>();
  for (const row of rows) for (const key of Object.keys(row)) if (!index.has(key.toLowerCase())) index.set(key.toLowerCase(), key);
  return index;
}

function resolve(index: Map<string, string>, field: string | string[] | undefined): string | undefined {
  for (const f of field == null ? [] : Array.isArray(field) ? field : [field]) {
    const found = index.get(f.toLowerCase());
    if (found) return found;
  }
  return undefined;
}

function blank(value: DBOrder[string] | undefined): boolean {
  return value == null || String(value).trim() === "";
}

export function formatQuik(value: DBOrder[string] | undefined, kind: QuikKind = "text", frac = 0): ReactNode {
  if (blank(value)) return <span className="cell-null">—</span>;
  const text = String(value).trim();
  switch (kind) {
    case "number": return formatNumber(text, frac);
    case "date": return formatDate(text);
    case "datetime": return formatDateTime(text);
    case "time": return formatTime(text);
    case "side": return <span className={sideClass(text)}>{text}</span>;
    case "state": return <span className={stateTone(text)}>{text}</span>;
    default: return text;
  }
}

/**
 * Chosen columns first, in the given order, then every other database column
 * hidden: nothing of the row is lost, it is one click away in the column menu.
 * A chosen column empty on every loaded row starts hidden too.
 */
export function quikColumns(rows: DBOrder[], spec: QuikColumn[]): TableColumn<DBOrder>[] {
  const index = fieldIndex(rows);
  const used = new Set<string>();
  const chosen: TableColumn<DBOrder>[] = [];
  for (const c of spec) {
    const field = resolve(index, c.field);
    if (!field) continue;
    const sub = resolve(index, c.sub);
    used.add(field);
    if (sub) used.add(sub);
    const kind = c.kind ?? "text";
    chosen.push({
      id: c.id, header: c.header, title: sub ? `${field} / ${sub}` : field,
      numeric: kind === "number",
      defaultHidden: c.hidden || (rows.length > 0 && rows.every(r => blank(r[field]))),
      value: r => dbSortValue(r[field]), exportValue: r => dbValue(r[field]),
      cell: r => {
        const href = c.link?.(r);
        let main: ReactNode = formatQuik(r[field], kind, c.frac);
        if (href && !blank(r[field])) main = <Link className="text-accent" to={href}>{main}</Link>;
        if (c.render) main = c.render(r, main);
        return sub && !blank(r[sub]) ? <span className="cell-stack">{main}<small>{formatQuik(r[sub], c.subKind)}</small></span> : main;
      },
    });
  }
  for (const field of index.values()) {
    if (used.has(field)) continue;
    chosen.push({ id: `raw:${field}`, header: field, title: field, defaultHidden: true, value: r => dbSortValue(r[field]), exportValue: r => dbValue(r[field]), cell: r => formatQuik(r[field]) });
  }
  return chosen;
}
