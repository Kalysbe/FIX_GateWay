import { useEffect, useState } from "react";
import { getOrderBook, type BookLevel, type OrderBook as Book } from "../lib/api";
import type { Side } from "../types";

type Props = {
  gatewayUrl: string;
  token: string;
  symbol: string;
  board: string;
  enabled: boolean;
  onPick?: (side: Side, price: string) => void;
};

function levels(book: Book | null, side: "bids" | "offers"): BookLevel[] {
  if (!book) return [];
  const list = book[side];
  if (list && list.length) return list;
  if (side === "bids" && book.bid) return [{ price: book.bid, size: book.bid_size }];
  if (side === "offers" && book.offer) return [{ price: book.offer, size: book.offer_size }];
  return [];
}

export default function OrderBook({ gatewayUrl, token, symbol, board, enabled, onPick }: Props) {
  const [book, setBook] = useState<Book | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled || !token || !symbol) {
      setBook(null);
      return;
    }
    let cancelled = false;
    const load = () => {
      void getOrderBook(gatewayUrl, token, symbol, board)
        .then((res) => {
          if (cancelled) return;
          setErr(null);
          setNote(res.note || null);
          setBook(res.books?.[0] || null);
        })
        .catch((e: Error) => {
          if (cancelled) return;
          setErr(e.message);
          setBook(null);
        });
    };
    load();
    const id = window.setInterval(load, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [gatewayUrl, token, symbol, board, enabled]);

  const bids = levels(book, "bids");
  const offers = levels(book, "offers");

  return (
    <div className="rounded-lg border border-white/10 bg-black/20 p-3">
      <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">Стакан</div>
      <div className="mb-2 font-mono text-[11px] text-slate-500">
        {symbol || "—"} · {board || "—"}
      </div>
      {!enabled && <div className="text-xs text-slate-500">Подключите сессию, чтобы увидеть заявки</div>}
      {err && <div className="text-xs text-danger">{err}</div>}
      {note && !err && <div className="text-xs text-slate-500">{note}</div>}
      {enabled && !err && (
        <div className="grid grid-cols-2 gap-2 text-xs">
          <div>
            <div className="mb-1 text-buy">Спрос</div>
            {bids.length === 0 && <div className="text-slate-500">нет заявок</div>}
            {bids.map((l, i) => (
              <button
                key={`b-${l.price}-${i}`}
                type="button"
                className="flex w-full justify-between font-mono tabular-nums hover:bg-white/5"
                onClick={() => l.price && onPick?.("BUY", l.price)}
              >
                <span className="text-buy">{l.price || "—"}</span>
                <span className="text-slate-500">{l.size || ""}</span>
              </button>
            ))}
          </div>
          <div>
            <div className="mb-1 text-sell">Предл.</div>
            {offers.length === 0 && <div className="text-slate-500">нет заявок</div>}
            {offers.map((l, i) => (
              <button
                key={`o-${l.price}-${i}`}
                type="button"
                className="flex w-full justify-between font-mono tabular-nums hover:bg-white/5"
                onClick={() => l.price && onPick?.("SELL", l.price)}
              >
                <span className="text-sell">{l.price || "—"}</span>
                <span className="text-slate-500">{l.size || ""}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
