import { useState, type FormEvent } from "react";
import { useDesk } from "../hooks/useDesk";
import type { OrderType, Side, TimeInForce } from "../types";

export default function OrderForm() {
  const { activeBroker, placeOrder, updateBroker } = useDesk();
  const [symbol, setSymbol] = useState("KG0105166315");
  const [board, setBoard] = useState("LISTEDSTOCKK");
  const [side, setSide] = useState<Side>("BUY");
  const [orderType, setOrderType] = useState<OrderType>("LIMIT");
  const [tif, setTif] = useState<TimeInForce>("DAY");
  const [qty, setQty] = useState("1");
  const [price, setPrice] = useState("400");
  const [clientOrderId, setClientOrderId] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  if (!activeBroker) {
    return (
      <div className="card text-sm text-slate-400">
        Создайте сессию слева, вставьте токен и подключитесь к gateway.
      </div>
    );
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!activeBroker) return;
    setBusy(true);
    setMsg(null);
    try {
      const order = await placeOrder(activeBroker.id, {
        symbol,
        board,
        side,
        orderType,
        timeInForce: tif,
        quantity: qty,
        price: orderType === "LIMIT" ? price : undefined,
        clientOrderId: clientOrderId || undefined,
        accountId: activeBroker.accountId,
        clientCode: activeBroker.clientCode,
      });
      if (order) {
        setMsg(`OK · ${order.clientOrderId} · ${order.status}`);
        setClientOrderId("");
      } else {
        setMsg("Не отправлено — смотрите Activity");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="font-display text-lg font-semibold">Новая заявка → Gateway</div>
          <div className="text-xs text-slate-400">
            от <span style={{ color: activeBroker.color }}>{activeBroker.name}</span>
            {" · "}
            <span className="font-mono">POST /api/v1/orders</span>
            {" · firm из токена (не из формы)"}
          </div>
        </div>
        <div className={`text-xs status-${activeBroker.connectionStatus}`}>
          {activeBroker.connectionStatus}
        </div>
      </div>

      <div className="mb-4 grid gap-3 md:grid-cols-2">
        <div>
          <label className="label">account_id (FIX 1)</label>
          <input
            className="input font-mono"
            placeholder="1-3101-006"
            value={activeBroker.accountId}
            onChange={(e) => updateBroker(activeBroker.id, { accountId: e.target.value })}
          />
        </div>
        <div>
          <label className="label">client_code (FIX 109)</label>
          <input
            className="input font-mono"
            placeholder="006"
            value={activeBroker.clientCode}
            onChange={(e) => updateBroker(activeBroker.id, { clientCode: e.target.value })}
          />
        </div>
      </div>

      <form className="grid gap-3 md:grid-cols-2 lg:grid-cols-3" onSubmit={(e) => void onSubmit(e)}>
        <div>
          <label className="label">Symbol / ISIN</label>
          <input className="input font-mono" value={symbol} onChange={(e) => setSymbol(e.target.value)} required />
        </div>
        <div>
          <label className="label">Board (класс)</label>
          <input className="input font-mono" value={board} onChange={(e) => setBoard(e.target.value)} required />
        </div>
        <div>
          <label className="label">client_order_id</label>
          <input
            className="input font-mono"
            placeholder="auto"
            value={clientOrderId}
            onChange={(e) => setClientOrderId(e.target.value)}
          />
        </div>
        <div>
          <label className="label">Side</label>
          <select className="input" value={side} onChange={(e) => setSide(e.target.value as Side)}>
            <option value="BUY">BUY</option>
            <option value="SELL">SELL</option>
          </select>
        </div>
        <div>
          <label className="label">Type</label>
          <select className="input" value={orderType} onChange={(e) => setOrderType(e.target.value as OrderType)}>
            <option value="LIMIT">LIMIT</option>
            <option value="MARKET">MARKET</option>
          </select>
        </div>
        <div>
          <label className="label">TIF</label>
          <select className="input" value={tif} onChange={(e) => setTif(e.target.value as TimeInForce)}>
            <option value="DAY">DAY</option>
            <option value="IOC">IOC</option>
            <option value="FOK">FOK</option>
            <option value="GTC">GTC</option>
          </select>
        </div>
        <div>
          <label className="label">Qty</label>
          <input className="input font-mono" value={qty} onChange={(e) => setQty(e.target.value)} required />
        </div>
        <div>
          <label className="label">Price</label>
          <input
            className="input font-mono"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            disabled={orderType !== "LIMIT"}
            required={orderType === "LIMIT"}
          />
        </div>
        <div className="flex items-end gap-2">
          <button className="btn w-full" type="submit" disabled={!activeBroker.connected || busy}>
            {busy ? "Отправка…" : "Отправить в Gateway"}
          </button>
        </div>
      </form>
      {msg && <div className="mt-3 font-mono text-xs text-slate-400">{msg}</div>}
    </div>
  );
}
