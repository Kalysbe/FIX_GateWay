import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { formatDateTime, sideLabel, statusLabel } from "../labels";

export default function OrdersPage() {
  const q = useQuery({
    queryKey: ["admin-orders"],
    queryFn: () => api<{ orders: any[] }>("/api/v1/admin/orders"),
    refetchInterval: 3000,
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Ордера</h1>
        <p className="text-sm text-slate-500">Книга ордеров шлюза по всем брокерам</p>
      </div>
      <div className="card overflow-x-auto bg-white">
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Брокер</th>
              <th>Фирма</th>
              <th>Код клиента</th>
              <th>Инструмент</th>
              <th>Сторона</th>
              <th>Кол-во</th>
              <th>Цена</th>
              <th>Статус</th>
              <th>Адаптер</th>
              <th>Создано</th>
            </tr>
          </thead>
          <tbody>
            {(q.data?.orders || []).map((o) => (
              <tr key={o.id}>
                <td>
                  <Link className="text-accent underline" to={`/orders/${o.id}`}>
                    {String(o.id).slice(0, 8)}
                  </Link>
                </td>
                <td>{o.broker_id}</td>
                <td>
                  <div>{o.firm_name || "—"}</div>
                  {o.quik_firm ? (
                    <div className="font-mono text-xs text-slate-500">{o.quik_firm}</div>
                  ) : null}
                </td>
                <td className="font-mono text-xs">{o.client_code || "—"}</td>
                <td>{o.symbol}</td>
                <td>{sideLabel(o.side)}</td>
                <td>{o.quantity}</td>
                <td>{o.price ?? "—"}</td>
                <td className={`status-${o.status}`}>{statusLabel(o.status)}</td>
                <td>{o.adapter_id || "—"}</td>
                <td>{formatDateTime(o.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
