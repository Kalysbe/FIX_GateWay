import { useRef, useState } from "react";
import { useDesk } from "../hooks/useDesk";

export default function SettingsPanel() {
  const { state, updateSettings, clearLog, resetAll, exportJson, importJson, refreshOrders } = useDesk();
  const fileRef = useRef<HTMLInputElement>(null);
  const s = state.settings;
  const [health, setHealth] = useState<string | null>(null);

  function download() {
    const blob = new Blob([exportJson()], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `broker-desk-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function checkHealth() {
    try {
      const base = (s.gatewayUrl || "").replace(/\/$/, "");
      const res = await fetch(`${base}/ready`);
      const body = await res.json().catch(() => ({}));
      setHealth(res.ok ? `ready OK · ${JSON.stringify(body)}` : `ready ${res.status}`);
    } catch (e) {
      setHealth(e instanceof Error ? e.message : "unreachable");
    }
  }

  return (
    <div className="card space-y-4">
      <div>
        <div className="font-display text-lg font-semibold">Gateway</div>
        <div className="text-xs text-slate-400">Реальные Bearer-токены · без локального mock</div>
      </div>

      <div>
        <label className="label">Gateway URL</label>
        <input
          className="input font-mono"
          placeholder="пусто = proxy → localhost:8080"
          value={s.gatewayUrl}
          onChange={(e) => updateSettings({ gatewayUrl: e.target.value })}
        />
      </div>

      <div>
        <label className="label">Poll interval (ms)</label>
        <input
          className="input font-mono"
          type="number"
          min={1000}
          value={s.pollIntervalMs}
          onChange={(e) => updateSettings({ pollIntervalMs: Number(e.target.value) || 2000 })}
        />
      </div>

      <div className="flex flex-wrap gap-2">
        <button className="btn-muted" onClick={() => void checkHealth()}>
          Check /ready
        </button>
        <button className="btn-muted" onClick={() => void refreshOrders()}>
          Refresh orders
        </button>
        <button className="btn-muted" onClick={download}>
          Export
        </button>
        <button className="btn-muted" onClick={() => fileRef.current?.click()}>
          Import
        </button>
        <input
          ref={fileRef}
          type="file"
          accept="application/json"
          className="hidden"
          onChange={async (e) => {
            const file = e.target.files?.[0];
            if (!file) return;
            importJson(await file.text());
            e.target.value = "";
          }}
        />
        <button className="btn-muted" onClick={clearLog}>
          Clear log
        </button>
        <button
          className="btn-danger"
          onClick={() => {
            if (confirm("Сбросить сессии и токены в localStorage?")) resetAll();
          }}
        >
          Reset all
        </button>
      </div>

      {health && <div className="break-all font-mono text-[11px] text-slate-400">{health}</div>}

      <div>
        <div className="mb-2 text-xs font-medium uppercase tracking-wide text-slate-400">Activity</div>
        <div className="max-h-48 space-y-1 overflow-y-auto font-mono text-[11px]">
          {state.activityLog.length === 0 && <div className="text-slate-500">пусто</div>}
          {state.activityLog.map((e) => (
            <div
              key={e.id}
              className={
                e.level === "error"
                  ? "text-danger"
                  : e.level === "warn"
                    ? "text-warn"
                    : e.level === "ok"
                      ? "text-buy"
                      : "text-slate-400"
              }
            >
              <span className="text-slate-600">{new Date(e.at).toLocaleTimeString()}</span> {e.message}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
