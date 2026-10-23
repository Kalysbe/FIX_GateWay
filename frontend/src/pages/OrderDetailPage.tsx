import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { api } from "../api";
import { liveRefetchInterval } from "../lib/poll";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { useAdminStream } from "../hooks/useAdminStream";
import { PageHeader } from "../components/PageHeader";
import { ErrorBanner, PageSpinner } from "../components/feedback";

export default function OrderDetailPage() {
  const { id } = useParams();
  const { connected } = useAdminStream();
  const q = useQuery({
    queryKey: ["order", id],
    queryFn: ({ signal }) => api<DBOrder>(`/api/v1/admin/orders/${encodeURIComponent(id ?? "")}`, { signal }, parseDBJSON),
    refetchInterval: liveRefetchInterval(connected, 2000),
  });
  if (q.isLoading) return <PageSpinner />;
  if (q.error) return <ErrorBanner error={q.error} onRetry={() => q.refetch()} />;
  return <div className="space-y-4">
    <PageHeader title="Заявка" />
    <div className="card overflow-x-auto">
      <table className="table"><tbody>{Object.entries(q.data ?? {}).map(([field, value]) =>
        <tr key={field}><th className="text-left font-mono">{field}</th><td className="whitespace-pre-wrap font-mono">{dbValue(value)}</td></tr>
      )}</tbody></table>
    </div>
  </div>;
}
