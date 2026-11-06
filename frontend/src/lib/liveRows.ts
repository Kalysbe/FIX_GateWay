import { useEffect, useRef, useState } from "react";
import type { SortValue } from "./table";

export type RowMark = "new" | "changed";
export type Trend = "up" | "down";
export type LiveMarks = { rows: Map<string, RowMark>; cells: Map<string, Trend>; added: number };

const EMPTY: LiveMarks = { rows: new Map(), cells: new Map(), added: 0 };
/** How long a new or changed row stays highlighted. */
export const LIVE_MARK_MS = 2600;

const toNumber = (v: SortValue) => typeof v === "number" ? v : typeof v === "string" && v.trim() !== "" && Number.isFinite(Number(v)) ? Number(v) : null;
export const cellKey = (row: string, column: string) => `${row}\u0000${column}`;

/**
 * Compares each refresh of a live table with the previous one: rows that
 * appeared are "new", rows whose data differs are "changed", and the trend
 * columns of a changed row say whether the value went up or down. The first
 * load, an empty result and a new `resetKey` (another filter or server page)
 * only set the baseline, so nothing flashes for data the user just asked for.
 */
export function useLiveMarks<T>({ rows, rowKey, enabled, resetKey, trend }: {
  rows: T[];
  rowKey: (row: T) => string | number;
  enabled: boolean;
  resetKey: string;
  trend: { id: string; value: (row: T) => SortValue }[];
}): LiveMarks {
  const previous = useRef<Map<string, { signature: string; values: (number | null)[] }> | null>(null);
  const baselineFor = useRef(resetKey);
  const [marks, setMarks] = useState<LiveMarks>(EMPTY);

  useEffect(() => {
    if (!enabled) return;
    if (baselineFor.current !== resetKey || rows.length === 0) {
      baselineFor.current = resetKey;
      previous.current = null;
      setMarks(EMPTY);
    }
    const before = previous.current;
    const next = new Map<string, { signature: string; values: (number | null)[] }>();
    const rowMarks = new Map<string, RowMark>();
    const cellMarks = new Map<string, Trend>();
    let added = 0;
    for (const row of rows) {
      const key = String(rowKey(row));
      const signature = JSON.stringify(row);
      const values = trend.map(t => toNumber(t.value(row)));
      next.set(key, { signature, values });
      const old = before?.get(key);
      if (!before) continue;
      if (!old) { rowMarks.set(key, "new"); added++; continue; }
      if (old.signature === signature) continue;
      rowMarks.set(key, "changed");
      trend.forEach((t, i) => {
        const a = old.values[i], b = values[i];
        if (a != null && b != null && a !== b) cellMarks.set(cellKey(key, t.id), b > a ? "up" : "down");
      });
    }
    previous.current = rows.length ? next : null;
    if (rowMarks.size) setMarks({ rows: rowMarks, cells: cellMarks, added });
    // `trend` and `rowKey` are recreated by callers on each render; the rows decide.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, enabled, resetKey]);

  // Each set of marks fades on its own timer, whatever the next refresh brings.
  useEffect(() => {
    if (marks === EMPTY) return;
    const timer = window.setTimeout(() => setMarks(EMPTY), LIVE_MARK_MS);
    return () => window.clearTimeout(timer);
  }, [marks]);

  return enabled ? marks : EMPTY;
}
