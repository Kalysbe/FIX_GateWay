import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { api, getToken } from "../api";
import { statusLabel, systemKeyLabel } from "../labels";

function isSimulator(a: { id?: string; name?: string; type?: string }): boolean {
  const type = String(a.type || "").toLowerCase();
  const id = String(a.id || "").toLowerCase();
  const name = String(a.name || "").toLowerCase();
  return type === "simulator" || id.includes("simulator") || name.includes("simulator");
}

function isHiddenAdapter(a: { id?: string; name?: string; type?: string }): boolean {
  const id = String(a.id || "").toLowerCase();
  const name = String(a.name || "").toLowerCase();
  return (
    isSimulator(a) ||
    id === "quik-fix-1b" ||
    (id.endsWith("b") && id.startsWith("quik-fix-")) ||
    name.includes("secondary") ||
    name.includes("резервный")
  );
}

function displayName(name: string): string {
  return name.replace(/\bprimary\b/gi, "основной");
}

type Dashboard = {
  system: { api: string; database: string; queue: string };
  adapters: Array<{
    id: string;
    name: string;
    status: string;
    active_orders: number;
    processed_orders: number;
    error_count: number;
    weight: number;
  }>;
  stats: { total: number; filled: number; rejected: number; active: number };
  brokers: number;
  active_accounts: number;
  adapters_available: number;
};

export default function DashboardPage() {
  const q = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<Dashboard>("/api/v1/admin/dashboard"),
    refetchInterval: 5000,
  });

  useEffect(() => {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const host = import.meta.env.VITE_API_URL ? new URL(import.meta.env.VITE_API_URL).host : location.host;
    const ws = new WebSocket(`${proto}://${host}/api/v1/admin/ws?token=${getToken()}`);
    ws.onmessage = () => q.refetch();
    return () => ws.close();
  }, [q]);

  if (q.isLoading) return <div>Загрузка…</div>;
  if (q.error) return <div className="text-danger">{(q.error as Error).message}</div>;
  const d = q.data!;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-2xl font-semibold">Состояние системы</h1>
        <p className="text-sm text-slate-500">Здоровье шлюза и поток ордеров в реальном времени</p>
      </div>
      <div className="grid gap-4 md:grid-cols-3">
        {Object.entries(d.system).map(([k, v]) => (
          <div key={k} className="card">
            <div className="text-xs uppercase tracking-wide text-slate-500">{systemKeyLabel(k)}</div>
            <div className={`mt-1 text-xl font-semibold status-${v}`}>{statusLabel(v)}</div>
          </div>
        ))}
      </div>
      <div className="grid gap-4 md:grid-cols-4">
        <Stat label="Ордеров сегодня" value={d.stats.total} />
        <Stat label="Исполнено" value={d.stats.filled} />
        <Stat label="Отклонено" value={d.stats.rejected} />
        <Stat label="Активных" value={d.stats.active} />
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="card">
          <h2 className="mb-3 font-medium">FIX-адаптеры</h2>
          <div className="space-y-2">
            {d.adapters.filter((a) => !isHiddenAdapter(a)).map((a) => (
              <div key={a.id} className="flex items-center justify-between rounded-lg bg-[#f4f7f8] px-3 py-2">
                <div>
                  <div className="font-medium">{displayName(a.name)}</div>
                  <div className="font-mono text-xs text-slate-500">
                    {a.id} · вес {a.weight}
                  </div>
                </div>
                <div className="text-right">
                  <div className={`font-semibold status-${a.status}`}>{statusLabel(a.status)}</div>
                  <div className="text-xs text-slate-500">
                    активных {a.active_orders} · готово {a.processed_orders}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="card space-y-3">
          <h2 className="font-medium">Парк</h2>
          <div className="flex justify-between text-sm">
            <span>Брокеры</span>
            <span>{d.brokers}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span>Активные счета</span>
            <span>{d.active_accounts}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span>Доступные адаптеры</span>
            <span>{d.adapters_available}</span>
          </div>
        </div>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="card">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 font-mono text-2xl font-semibold">{value ?? 0}</div>
    </div>
  );
}
