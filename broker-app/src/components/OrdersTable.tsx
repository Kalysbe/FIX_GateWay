import { useMemo, useState } from "react";
import { useDesk } from "../hooks/useDesk";

const OPEN = new Set(["QUEUED", "SENT", "ROUTING", "ACKNOWLEDGED", "PARTIALLY_FILLED", "CANCEL_PENDING"]);

export default function OrdersTable() {
  const { state, activeBroker, cancelOrder, clearOrders, setActiveBrokerId, refreshOrders } = useDesk();
  const [filter, setFilter] = useState<"active" | "all" | "mine">("all");
  const [busyId, setBusyId] = useState<string | null>(null);

  const brokerMap = useMemo(() => new Map(state.brokers.map((b) => [b.id, b])), [state.brokers]);

  const rows = useMemo(() => {
    let list = state.orders;
    if (filter === "mine" && activeBroker) list = list.filter((o) => o.brokerId === activeBroker.id);
    if (filter === "active") list = list.filter((o) => OPEN.has(o.status));
    return list;
  }, [state.orders, filter, activeBroker]);

  return (
    <div className="card overflow-hidden">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <div className="font-display text-lg font-semibold">Заявки (Gateway)</div>
          <div className="text-xs text-slate-400">
            {rows.length} показано · poll {state.settings.pollIntervalMs}ms
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {(["all", "active", "mine"] as const).map((f) => (
            <button
              key={f}
              className={filter === f ? "btn px-2 py-1 text-xs" : "btn-muted px-2 py-1 text-xs"}
              onClick={() => setFilter(f)}
            >
              {f}
            </button>
          ))}
          <button className="btn-muted px-2 py-1 text-xs" onClick={() => void refreshOrders()}>
            Refresh
          </button>
          <button className="btn-muted px-2 py-1 text-xs" onClick={clearOrders}>
            Clear local
          </button>
        </div>
      </div>

      <div className="max-h-[420px] overflow-auto">
        <table className="table">
          <thead>
            <tr>
              <th>Broker</th>
              <th>ClOrdID</th>
              <th>Symbol</th>
              <th>Side</th>
              <th>Qty</th>
              <th>Px</th>
              <th>Status</th>
              <th>Filled</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={9} className="py-8 text-center text-slate-500">
                  Нет заявок — подключите токен и отправьте ордер
                </td>
              </tr>
            )}
            {rows.map((o) => {
              const b = brokerMap.get(o.brokerId);
              return (
                <tr key={o.id}>
                  <td>
                    <button
                      className="inline-flex items-center gap-1.5 hover:underline"
                      onClick={() => setActiveBrokerId(o.brokerId)}
                    >
                      <span className="h-2 w-2 rounded-full" style={{ background: b?.color || "#94a3b8" }} />
                      <span className="font-mono text-xs">{b?.code || "?"}</span>
                    </button>
                  </td>
                  <td className="font-mono text-xs">{o.clientOrderId}</td>
                  <td className="font-mono text-xs">{o.symbol}</td>
                  <td className={o.side === "BUY" ? "text-buy" : "text-sell"}>{o.side}</td>
                  <td className="font-mono">{o.quantity}</td>
                  <td className="font-mono">{o.price || "MKT"}</td>
                  <td className={`status-${o.status} font-mono text-xs`}>{o.status}</td>
                  <td className="font-mono text-xs">
                    {o.filledQty}
                    {o.avgPrice ? ` @ ${o.avgPrice}` : ""}
                  </td>
                  <td>
                    {OPEN.has(o.status) && o.status !== "CANCEL_PENDING" && (
                      <button
                        className="btn-danger px-2 py-1 text-xs"
                        disabled={busyId === o.id}
                        onClick={() => {
                          setBusyId(o.id);
                          void cancelOrder(o.id).finally(() => setBusyId(null));
                        }}
                      >
                        Cancel
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
