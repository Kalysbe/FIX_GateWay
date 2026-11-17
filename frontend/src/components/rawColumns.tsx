import type { TableColumn } from "./DataTable";
import { dbSortValue, dbValue, type DBOrder } from "../lib/rawOrders";

// Columns of QUIK export rows exactly as stored: database column names in
// first-seen order, values in their source text.
export function rawFields(rows: DBOrder[]): string[] {
  return Array.from(new Set(rows.flatMap(r => Object.keys(r))));
}

export function rawColumns(rows: DBOrder[]): TableColumn<DBOrder>[] {
  return rawFields(rows).map(field => ({
    id: field, header: field, value: r => dbSortValue(r[field]), exportValue: r => dbValue(r[field]),
    cell: r => <span className="whitespace-pre-wrap">{dbValue(r[field])}</span>,
  }));
}

export function rawSearchText(rows: DBOrder[]): (row: DBOrder) => string {
  const fields = rawFields(rows);
  return r => fields.map(f => dbValue(r[f])).join(" ");
}
