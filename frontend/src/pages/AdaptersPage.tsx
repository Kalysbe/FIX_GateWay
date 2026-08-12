import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { statusLabel } from "../labels";

function isSimulator(a: any): boolean {
  const type = String(a.Type || a.type || "").toLowerCase();
  const id = String(a.ID || a.id || "").toLowerCase();
  const name = String(a.Name || a.name || "").toLowerCase();
  return type === "simulator" || id.includes("simulator") || name.includes("simulator");
}

function isHiddenAdapter(a: any): boolean {
  const id = String(a.ID || a.id || "").toLowerCase();
  const name = String(a.Name || a.name || "").toLowerCase();
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

export default function AdaptersPage() {
  const q = useQuery({
    queryKey: ["adapters"],
    queryFn: () => api<{ adapters: any[] }>("/api/v1/admin/adapters"),
    refetchInterval: 3000,
  });

  const adapters = (q.data?.adapters || []).filter((a) => !isHiddenAdapter(a));

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">FIX-адаптеры</h1>
        <p className="text-sm text-slate-500">Пул адаптеров в рантайме</p>
      </div>
      <div className="grid gap-4">
        {adapters.map((a) => (
          <div key={a.ID || a.id} className="card bg-white">
            <div className="text-lg font-semibold text-[#1a2332]">
              {displayName(a.Name || a.name)}
            </div>
            <div className="font-mono text-xs text-slate-500">
              {a.ID || a.id} · {a.Type || a.type} · {a.Host || a.host}:{a.Port || a.port}
            </div>
            <div className="mt-2 flex flex-wrap gap-4 text-sm text-[#1a2332]">
              <span className={`font-semibold status-${a.Status || a.status}`}>
                {statusLabel(a.Status || a.status)}
              </span>
              <span>вес {a.Weight || a.weight}</span>
              <span>активных {a.ActiveOrders || a.active_orders}</span>
              <span>обработано {a.ProcessedOrders || a.processed_orders}</span>
              <span>ошибок {a.ErrorCount || a.error_count}</span>
            </div>
          </div>
        ))}
        {!q.isLoading && adapters.length === 0 && (
          <div className="card bg-white text-sm text-slate-500">Нет активных адаптеров</div>
        )}
      </div>
    </div>
  );
}
