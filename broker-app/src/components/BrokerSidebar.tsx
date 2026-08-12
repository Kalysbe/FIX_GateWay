import { useState } from "react";
import { useDesk } from "../hooks/useDesk";
import { maskToken } from "../lib/storage";

export default function BrokerSidebar() {
  const {
    state,
    activeBroker,
    connectedCount,
    setActiveBrokerId,
    addBroker,
    connect,
    disconnect,
    connectAll,
    disconnectAll,
    removeBroker,
    updateBroker,
  } = useDesk();

  const [showTokenFor, setShowTokenFor] = useState<string | null>(null);

  return (
    <aside className="card flex h-full flex-col gap-4">
      <div>
        <div className="font-display text-lg font-semibold tracking-tight">Брокеры</div>
        <div className="mt-1 text-xs text-slate-400">
          {connectedCount}/{state.brokers.length} online · токены в localStorage · API gateway
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        <button className="btn" onClick={() => addBroker()}>
          + Сессия
        </button>
        <button className="btn-muted" onClick={() => void connectAll()} disabled={!state.brokers.length}>
          Connect all
        </button>
        <button className="btn-muted" onClick={disconnectAll} disabled={!connectedCount}>
          Disconnect all
        </button>
      </div>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
        {state.brokers.length === 0 && (
          <div className="rounded-lg border border-dashed border-white/15 p-4 text-sm text-slate-400">
            Создайте сессию, вставьте API-токен из Admin → Brokers → Tokens и нажмите Connect.
          </div>
        )}
        {state.brokers.map((b) => {
          const active = activeBroker?.id === b.id;
          const orders = state.orders.filter((o) => o.brokerId === b.id).length;
          const hasToken = Boolean(b.apiToken.trim());
          return (
            <div
              key={b.id}
              role="button"
              tabIndex={0}
              onClick={() => setActiveBrokerId(b.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") setActiveBrokerId(b.id);
              }}
              className={`w-full rounded-xl border p-3 text-left transition ${
                active ? "border-accent/50 bg-accent/10" : "border-white/10 bg-black/20 hover:border-white/20"
              }`}
            >
              <div className="flex items-start justify-between gap-2">
                <div className="flex items-center gap-2">
                  <span className="h-3 w-3 rounded-full" style={{ background: b.color }} />
                  <div>
                    <input
                      className="w-full bg-transparent font-medium outline-none"
                      value={b.name}
                      onClick={(e) => e.stopPropagation()}
                      onChange={(e) => updateBroker(b.id, { name: e.target.value })}
                    />
                    <input
                      className="w-full bg-transparent font-mono text-xs text-slate-400 outline-none"
                      value={b.code}
                      onClick={(e) => e.stopPropagation()}
                      onChange={(e) => updateBroker(b.id, { code: e.target.value })}
                    />
                  </div>
                </div>
                <div className="flex items-center gap-1.5 text-xs">
                  <span className={`dot dot-${b.connectionStatus}`} />
                  <span className={`status-${b.connectionStatus}`}>{b.connectionStatus}</span>
                </div>
              </div>

              <div className="mt-2" onClick={(e) => e.stopPropagation()}>
                <label className="label">API token</label>
                <div className="flex gap-1">
                  <input
                    className="input font-mono text-xs"
                    type={showTokenFor === b.id ? "text" : "password"}
                    placeholder="sk_live_… или sk_test_…"
                    value={b.apiToken}
                    disabled={b.connected}
                    onChange={(e) => updateBroker(b.id, { apiToken: e.target.value })}
                  />
                  <button
                    type="button"
                    className="btn-muted px-2 text-xs"
                    onClick={() => setShowTokenFor(showTokenFor === b.id ? null : b.id)}
                  >
                    {showTokenFor === b.id ? "Hide" : "Show"}
                  </button>
                </div>
                {hasToken && !showTokenFor && (
                  <div className="mt-1 font-mono text-[10px] text-slate-500">{maskToken(b.apiToken)}</div>
                )}
              </div>

              <div className="mt-2 grid grid-cols-2 gap-2" onClick={(e) => e.stopPropagation()}>
                <div>
                  <label className="label">account_id</label>
                  <input
                    className="input font-mono text-xs"
                    placeholder="1-3101-006"
                    value={b.accountId}
                    onChange={(e) => updateBroker(b.id, { accountId: e.target.value })}
                  />
                </div>
                <div>
                  <label className="label">client_code</label>
                  <input
                    className="input font-mono text-xs"
                    placeholder="006"
                    value={b.clientCode}
                    onChange={(e) => updateBroker(b.id, { clientCode: e.target.value })}
                  />
                </div>
              </div>

              {b.lastError && (
                <div className="mt-2 text-xs text-danger">{b.lastError}</div>
              )}

              <div className="mt-2 flex items-center justify-between text-xs text-slate-400">
                <span>
                  {b.accountId && b.clientCode ? "account/code OK" : "заполните account + client_code"}
                </span>
                <span>{orders} orders</span>
              </div>

              <div className="mt-2 flex gap-2" onClick={(e) => e.stopPropagation()}>
                {b.connected ? (
                  <button className="btn-muted px-2 py-1 text-xs" onClick={() => disconnect(b.id)}>
                    Disconnect
                  </button>
                ) : (
                  <button
                    className="btn px-2 py-1 text-xs"
                    disabled={!hasToken || b.connectionStatus === "CONNECTING"}
                    onClick={() => void connect(b.id)}
                  >
                    {b.connectionStatus === "CONNECTING" ? "…" : "Connect"}
                  </button>
                )}
                <button className="btn-danger px-2 py-1 text-xs" onClick={() => removeBroker(b.id)}>
                  Delete
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </aside>
  );
}
