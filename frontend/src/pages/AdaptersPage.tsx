import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { displayAdapterName, normalizeAdapter } from "../lib/adapters";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, PageSpinner } from "../components/feedback";
import { StatusBadge } from "../components/StatusBadge";
import type { Adapter } from "../types/admin";

export default function AdaptersPage() {
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["adapters"],
    queryFn: ({ signal }) => api<{ adapters: Adapter[] }>("/api/v1/admin/adapters", { signal }),
    refetchInterval: liveRefetchInterval(connected, 3000),
  });

  const adapters = (q.data?.adapters || []).map(normalizeAdapter);

  return (
    <div className="space-y-6">
      <PageHeader title="FIX-адаптеры" subtitle="Пул адаптеров в рантайме" />
      {q.error && <ErrorBanner error={q.error} onRetry={() => q.refetch()} />}
      {q.isLoading && <PageSpinner />}
      <div className="grid gap-4">
        {adapters.map((a) => (
          <div key={a.id} className="card">
            <div className="text-lg font-semibold">{displayAdapterName(a.name)}</div>
            <div className="font-mono text-xs text-slate-500">
              {a.id} · {a.type} · {a.host}:{a.port}
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-4 text-sm">
              <StatusBadge status={a.enabled ? a.status : "DISABLED"} />
              <span>вес {a.weight}</span>
              <span>активных {a.active_orders}</span>
              <span>обработано {a.processed_orders}</span>
              <span>ошибок {a.error_count}</span>
            </div>
          </div>
        ))}
        {!q.isLoading && adapters.length === 0 && <EmptyState title="Нет активных адаптеров" />}
      </div>
    </div>
  );
}
