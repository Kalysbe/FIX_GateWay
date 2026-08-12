import { useEffect, useMemo, useRef, useState } from "react";
import { useDesk } from "../hooks/useDesk";
import type { OrderType, Side } from "../types";

function randInt(min: number, max: number): number {
  const a = Math.min(min, max);
  const b = Math.max(min, max);
  return Math.floor(a + Math.random() * (b - a + 1));
}

function randFloat(min: number, max: number, decimals = 2): string {
  const a = Math.min(min, max);
  const b = Math.max(min, max);
  const v = a + Math.random() * (b - a);
  return v.toFixed(decimals);
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new DOMException("aborted", "AbortError"));
      return;
    }
    const t = window.setTimeout(() => resolve(), ms);
    const onAbort = () => {
      window.clearTimeout(t);
      reject(new DOMException("aborted", "AbortError"));
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

type Stats = {
  sent: number;
  ok: number;
  fail: number;
  startedAt?: number;
  endsAt?: number;
};

export default function ConcurrentPanel() {
  const { connectedCount, placeOrder, state } = useDesk();
  const [symbol, setSymbol] = useState("KG0105166315");
  const [board, setBoard] = useState("LISTEDSTOCKK");
  const [orderType, setOrderType] = useState<OrderType>("LIMIT");
  const [qtyMin, setQtyMin] = useState(1);
  const [qtyMax, setQtyMax] = useState(5);
  const [priceMin, setPriceMin] = useState(350);
  const [priceMax, setPriceMax] = useState(450);
  const [durationSec, setDurationSec] = useState(30);
  const [intervalMinMs, setIntervalMinMs] = useState(200);
  const [intervalMaxMs, setIntervalMaxMs] = useState(1200);
  const [running, setRunning] = useState(false);
  const [leftSec, setLeftSec] = useState(0);
  const [stats, setStats] = useState<Stats>({ sent: 0, ok: 0, fail: 0 });
  const abortRef = useRef<AbortController | null>(null);

  const readyBrokers = useMemo(
    () =>
      state.brokers.filter(
        (b) => b.connected && b.apiToken.trim() && b.accountId.trim() && b.clientCode.trim(),
      ),
    [state.brokers],
  );

  const missing = useMemo(() => {
    return state.brokers
      .filter((b) => b.connected)
      .filter((b) => !b.accountId.trim() || !b.clientCode.trim())
      .map((b) => b.name);
  }, [state.brokers]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  useEffect(() => {
    if (!running || !stats.endsAt) {
      setLeftSec(0);
      return;
    }
    const tick = () => {
      setLeftSec(Math.max(0, Math.ceil((stats.endsAt! - Date.now()) / 1000)));
    };
    tick();
    const id = window.setInterval(tick, 200);
    return () => window.clearInterval(id);
  }, [running, stats.endsAt]);

  async function startTest() {
    if (readyBrokers.length === 0 || running) return;
    const ac = new AbortController();
    abortRef.current = ac;
    const endsAt = Date.now() + durationSec * 1000;
    setRunning(true);
    setStats({ sent: 0, ok: 0, fail: 0, startedAt: Date.now(), endsAt });

    let seq = 0;
    try {
      while (!ac.signal.aborted && Date.now() < endsAt) {
        const broker = readyBrokers[Math.floor(Math.random() * readyBrokers.length)];
        if (!broker) break;
        if (ac.signal.aborted) break;

        const side: Side = Math.random() < 0.5 ? "BUY" : "SELL";
        const qty = String(randInt(qtyMin, qtyMax));
        const price = orderType === "LIMIT" ? randFloat(priceMin, priceMax, 2) : undefined;
        seq += 1;

        setStats((s) => ({ ...s, sent: s.sent + 1 }));
        const order = await placeOrder(broker.id, {
          symbol,
          board,
          side,
          orderType,
          timeInForce: "DAY",
          quantity: qty,
          price,
          clientOrderId: `${broker.code}-T${Date.now()}-${seq}`,
          accountId: broker.accountId,
          clientCode: broker.clientCode,
        });

        if (ac.signal.aborted) break;

        setStats((s) =>
          order ? { ...s, ok: s.ok + 1 } : { ...s, fail: s.fail + 1 },
        );

        const wait = randInt(intervalMinMs, intervalMaxMs);
        const remaining = endsAt - Date.now();
        if (remaining <= 0) break;
        await sleep(Math.min(wait, remaining), ac.signal);
      }
    } catch (e) {
      if (!(e instanceof DOMException && e.name === "AbortError")) {
        console.error(e);
      }
    } finally {
      setRunning(false);
      abortRef.current = null;
    }
  }

  function stopTest() {
    abortRef.current?.abort();
  }

  return (
    <div className="card">
      <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="font-display text-lg font-semibold">Рандомный нагрузочный тест</div>
          <div className="text-xs text-slate-400">
            Side / цена / qty / брокер — случайно · сначала заполните account_id и client_code у каждой сессии
          </div>
        </div>
        {running && (
          <div className="rounded-lg border border-warn/40 bg-warn/10 px-3 py-1.5 font-mono text-sm text-warn">
            осталось {leftSec}s
          </div>
        )}
      </div>

      <div className="mb-4 overflow-x-auto rounded-lg border border-white/10">
        <table className="table text-xs">
          <thead>
            <tr>
              <th>Брокер</th>
              <th>Status</th>
              <th>account_id</th>
              <th>client_code</th>
              <th>Готов</th>
            </tr>
          </thead>
          <tbody>
            {state.brokers.length === 0 && (
              <tr>
                <td colSpan={5} className="py-4 text-center text-slate-500">
                  Нет сессий
                </td>
              </tr>
            )}
            {state.brokers.map((b) => {
              const ready =
                b.connected && b.apiToken.trim() && b.accountId.trim() && b.clientCode.trim();
              return (
                <tr key={b.id}>
                  <td>
                    <span className="mr-1.5 inline-block h-2 w-2 rounded-full" style={{ background: b.color }} />
                    {b.name}
                  </td>
                  <td className={`status-${b.connectionStatus}`}>{b.connectionStatus}</td>
                  <td className="font-mono">{b.accountId || "—"}</td>
                  <td className="font-mono">{b.clientCode || "—"}</td>
                  <td className={ready ? "text-buy" : "text-warn"}>{ready ? "YES" : "NO"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {missing.length > 0 && (
        <div className="mb-3 text-xs text-warn">
          Online без account/client_code: {missing.join(", ")} — заполните в карточке слева
        </div>
      )}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div>
          <label className="label">Symbol</label>
          <input className="input font-mono" value={symbol} onChange={(e) => setSymbol(e.target.value)} disabled={running} />
        </div>
        <div>
          <label className="label">Board</label>
          <input className="input font-mono" value={board} onChange={(e) => setBoard(e.target.value)} disabled={running} />
        </div>
        <div>
          <label className="label">Type</label>
          <select
            className="input"
            value={orderType}
            disabled={running}
            onChange={(e) => setOrderType(e.target.value as OrderType)}
          >
            <option value="LIMIT">LIMIT (цена рандом)</option>
            <option value="MARKET">MARKET</option>
          </select>
        </div>
        <div>
          <label className="label">Длительность теста (сек)</label>
          <input
            className="input font-mono"
            type="number"
            min={5}
            max={3600}
            value={durationSec}
            disabled={running}
            onChange={(e) => setDurationSec(Math.max(5, Number(e.target.value) || 5))}
          />
        </div>
        <div>
          <label className="label">Qty min</label>
          <input
            className="input font-mono"
            type="number"
            min={1}
            value={qtyMin}
            disabled={running}
            onChange={(e) => setQtyMin(Math.max(1, Number(e.target.value) || 1))}
          />
        </div>
        <div>
          <label className="label">Qty max</label>
          <input
            className="input font-mono"
            type="number"
            min={1}
            value={qtyMax}
            disabled={running}
            onChange={(e) => setQtyMax(Math.max(1, Number(e.target.value) || 1))}
          />
        </div>
        <div>
          <label className="label">Price min</label>
          <input
            className="input font-mono"
            type="number"
            step="0.01"
            value={priceMin}
            disabled={running || orderType !== "LIMIT"}
            onChange={(e) => setPriceMin(Number(e.target.value) || 0)}
          />
        </div>
        <div>
          <label className="label">Price max</label>
          <input
            className="input font-mono"
            type="number"
            step="0.01"
            value={priceMax}
            disabled={running || orderType !== "LIMIT"}
            onChange={(e) => setPriceMax(Number(e.target.value) || 0)}
          />
        </div>
        <div>
          <label className="label">Пауза между заявками min (ms)</label>
          <input
            className="input font-mono"
            type="number"
            min={0}
            value={intervalMinMs}
            disabled={running}
            onChange={(e) => setIntervalMinMs(Math.max(0, Number(e.target.value) || 0))}
          />
        </div>
        <div>
          <label className="label">Пауза max (ms)</label>
          <input
            className="input font-mono"
            type="number"
            min={0}
            value={intervalMaxMs}
            disabled={running}
            onChange={(e) => setIntervalMaxMs(Math.max(0, Number(e.target.value) || 0))}
          />
        </div>
        <div className="flex items-end gap-2 sm:col-span-2">
          {!running ? (
            <button
              className="btn w-full"
              disabled={readyBrokers.length === 0}
              onClick={() => void startTest()}
            >
              Старт теста · {readyBrokers.length}/{connectedCount} брокеров готовы
            </button>
          ) : (
            <button className="btn-danger w-full" onClick={stopTest}>
              Стоп
            </button>
          )}
        </div>
      </div>

      <div className="mt-3 flex flex-wrap gap-4 font-mono text-xs text-slate-400">
        <span>
          sent <span className="text-slate-200">{stats.sent}</span>
        </span>
        <span>
          ok <span className="text-buy">{stats.ok}</span>
        </span>
        <span>
          fail <span className="text-danger">{stats.fail}</span>
        </span>
        <span>BUY/SELL = random · брокер = random</span>
      </div>
    </div>
  );
}
