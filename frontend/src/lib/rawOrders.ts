// Preserve the lexical form of database numbers for display. The wire JSON
// remains unchanged; no Number/float conversion can round a 19-digit OrderNum.
export type DBOrder = Record<string, string | boolean | null>;

export function parseDBJSON<T>(text: string): T {
  return JSON.parse(text.replace(/"(?:\\.|[^"\\])*"|(-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
    (token, number: string | undefined) => number === undefined ? token : JSON.stringify(number))) as T;
}

export function dbValue(value: DBOrder[string] | undefined): string {
  return value == null ? "null" : String(value);
}

// Sorting only: decimals that fit a double compare as numbers (the display
// value stays the source text). Longer integers keep the string, which the
// table's numeric collator orders correctly digit by digit.
export function dbSortValue(value: DBOrder[string] | undefined): string | number | null {
  if (value == null || typeof value === "boolean") return value == null ? null : String(value);
  const m = /^-?(\d+)(?:\.\d+)?$/.exec(value);
  return m && m[1].length <= 15 ? Number(value) : value;
}

/** Empty when the row lacks a part of the Orders primary key; such rows get no link. */
export function orderKey(order: DBOrder): string {
  const date = String(order.TradeDate ?? "").slice(0, 10).replaceAll("-", "");
  if (date.length !== 8 || order.ClassCode == null || order.OrderNum == null) return "";
  return `quik-${date}-${order.ClassCode}-${order.OrderNum}`;
}

export function orderRowKey(order: DBOrder): string {
  return orderKey(order) || JSON.stringify(order);
}

export const ORDER_PAGE_SIZE = 200;

/** Server paging headers; a missing header means no further page. */
export function nextOrderOffset(headers: Headers): number | null {
  if (headers.get("X-Has-More") !== "true") return null;
  const next = Number(headers.get("X-Next-Offset"));
  return Number.isInteger(next) && next > 0 ? next : null;
}
