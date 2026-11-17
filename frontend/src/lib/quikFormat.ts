// Display-only formatting of QUIK export values. The text is reshaped as a
// string, never through Number, so long numbers keep every digit; CSV export
// and the API keep the source text.

const NUMBER = /^(-?)(\d+)(?:\.(\d+))?$/;
const DATE = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2}):(\d{2}))?/;

/** "6500.00000000" → "6 500", with at least minFrac decimals: "95" → "95,00". */
export function formatNumber(raw: string, minFrac = 0): string {
  const m = NUMBER.exec(raw);
  if (!m) return raw;
  const frac = (m[3] ?? "").replace(/0+$/, "").padEnd(minFrac, "0");
  const int = m[2].replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  return `${m[1]}${int}${frac ? `,${frac}` : ""}`;
}

/** Calendar date "dd.mm.yyyy". */
export function formatDate(raw: string): string {
  const m = DATE.exec(raw);
  return m ? `${m[3]}.${m[2]}.${m[1]}` : raw;
}

/** "dd.mm.yy hh:mm:ss"; midnight is a bare date. */
export function formatDateTime(raw: string): string {
  const m = DATE.exec(raw);
  if (!m) return raw;
  const date = `${m[3]}.${m[2]}.${m[1].slice(2)}`;
  return m[4] == null || `${m[4]}${m[5]}${m[6]}` === "000000" ? date : `${date} ${m[4]}:${m[5]}:${m[6]}`;
}

/** Time of day only: QUIK keeps Params."time" on 1900-01-01. */
export function formatTime(raw: string): string {
  const m = DATE.exec(raw);
  return m?.[4] == null ? raw : `${m[4]}:${m[5]}:${m[6]}`;
}

/** Badge tone of a QUIK order or trading state. */
export function stateTone(state: string): string {
  const s = state.trim().toLowerCase();
  if (s.startsWith("исполн")) return "badge-success";
  if (s.startsWith("актив") || s === "открыта" || s === "торгуется") return "badge-info";
  if (s.startsWith("отклон") || s.startsWith("ошиб")) return "badge-danger";
  return "badge-muted";
}

/** Buy or sell colour of QUIK "Operation". */
export function sideClass(operation: string): string {
  const s = operation.trim().toLowerCase();
  return s.startsWith("куп") || s.startsWith("buy") ? "side-buy" : s.startsWith("прод") || s.startsWith("sell") ? "side-sell" : "";
}
