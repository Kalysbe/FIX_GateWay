import { Link } from "react-router-dom";
import { dbSortValue, dbValue, orderKey, type DBOrder } from "./rawOrders";
import type { TableColumn } from "../components/DataTable";

// Quotes are QUIK Orders rows still in the book (State = "Активна"), as stored.

function fmt(v: DBOrder[string] | undefined, digits = 2): string {
  if (v == null || v === "") return "—";
  const n = Number(v);
  if (!Number.isFinite(n)) return String(v);
  return n.toLocaleString("ru-RU", { minimumFractionDigits: 0, maximumFractionDigits: digits });
}

export function isBuy(op: DBOrder[string] | undefined): boolean {
  const s = dbValue(op).toLowerCase();
  return s === "buy" || s.includes("купл");
}

/** The current-trades page of one instrument. */
export function instrumentPath(board: DBOrder[string] | undefined, symbol: DBOrder[string] | undefined): string {
  return `/market-data/${encodeURIComponent(dbValue(board).trim())}/${encodeURIComponent(dbValue(symbol).trim())}`;
}

export function quoteColumns(): TableColumn<DBOrder>[] {
  const num = (field: string, header: string, digits = 2): TableColumn<DBOrder> =>
    ({ id: field, header, value: r => dbSortValue(r[field]), exportValue: r => dbValue(r[field]), cell: r => fmt(r[field], digits), numeric: true });
  return [
    { id: "operation", header: "Операция", value: r => dbValue(r.Operation), cell: r => <span className={isBuy(r.Operation) ? "badge-success" : "badge-danger"}>{r.Operation == null ? "—" : dbValue(r.Operation)}</span> },
    { id: "number", header: "№ заявки", value: r => dbSortValue(r.OrderNum), exportValue: r => dbValue(r.OrderNum), className: "font-mono",
      cell: r => orderKey(r) ? <Link className="text-accent" to={`/orders/${encodeURIComponent(orderKey(r))}`}>{dbValue(r.OrderNum)}</Link> : dbValue(r.OrderNum) },
    { id: "firm", header: "Фирма", value: r => dbValue(r.FirmId), className: "font-mono" },
    { id: "class", header: "Класс", value: r => dbValue(r.ClassCode), className: "font-mono" },
    { id: "symbol", header: "Инструмент", value: r => dbValue(r.SecCode), className: "font-mono font-medium",
      cell: r => r.ClassCode != null && r.SecCode != null ? <Link className="text-accent" to={instrumentPath(r.ClassCode, r.SecCode)}>{dbValue(r.SecCode)}</Link> : dbValue(r.SecCode) },
    num("Price", "Цена"),
    num("Value", "Сумма"),
    num("Qty", "Количество", 0),
    num("Balance", "Остаток", 0),
    { id: "date", header: "Дата", value: r => dbValue(r.TradeDate), cell: r => dbValue(r.TradeDate).slice(0, 10), className: "font-mono" },
  ];
}
