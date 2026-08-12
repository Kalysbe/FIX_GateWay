import { FormEvent, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { statusLabel } from "../labels";

export default function BrokerDetailPage() {
  const { id } = useParams();
  const qc = useQueryClient();
  const broker = useQuery({ queryKey: ["broker", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}`) });
  const accounts = useQuery({ queryKey: ["accounts", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}/accounts`) });
  const security = useQuery({ queryKey: ["security", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}/security`) });
  const [accCode, setAccCode] = useState("001");
  const [tradingAccount, setTradingAccount] = useState("L01+00000F00");

  async function createAccount(e: FormEvent) {
    e.preventDefault();
    await api(`/api/v1/admin/brokers/${id}/accounts`, {
      method: "POST",
      body: JSON.stringify({ code: accCode, name: accCode, quik_firm: "FIRM", trading_account: tradingAccount }),
    });
    qc.invalidateQueries({ queryKey: ["accounts", id] });
  }

  if (broker.isLoading) return <div>Загрузка…</div>;
  const b = broker.data?.broker;
  const st = security.data?.security;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">{b?.name}</h1>
          <p className="font-mono text-sm text-slate-500">
            {b?.code} · {statusLabel(b?.status)}
          </p>
        </div>
        <Link className="btn" to={`/brokers/${id}/tokens`}>
          Управление API-токенами
        </Link>
      </div>

      <div className="grid gap-3 md:grid-cols-4">
        {[
          ["Активные токены", st?.active_tokens],
          ["Отозванные токены", st?.revoked_tokens],
          ["Неудачные входы 24ч", st?.failed_auth_attempts_24h],
          ["IP-whitelist", st?.ip_whitelist_enabled ? `ВКЛ (${st?.ip_whitelist_count || 0})` : "ВЫКЛ"],
        ].map(([label, value]) => (
          <div key={String(label)} className="card py-3">
            <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
            <div className="mt-1 text-lg font-semibold">{value ?? "—"}</div>
          </div>
        ))}
      </div>

      <div className="card space-y-3">
        <h2 className="font-medium">Торговые счета</h2>
        <form onSubmit={createAccount} className="grid gap-2 md:grid-cols-3">
          <input className="input" placeholder="Код" value={accCode} onChange={(e) => setAccCode(e.target.value)} />
          <input
            className="input"
            placeholder="Торговый счёт"
            value={tradingAccount}
            onChange={(e) => setTradingAccount(e.target.value)}
          />
          <button className="btn" type="submit">
            Добавить счёт
          </button>
        </form>
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Код</th>
              <th>Торговый счёт</th>
              <th>Статус</th>
            </tr>
          </thead>
          <tbody>
            {(accounts.data?.accounts || []).map((a: any) => (
              <tr key={a.ID || a.id}>
                <td>{a.ID || a.id}</td>
                <td>{a.Code || a.code}</td>
                <td className="font-mono">{a.TradingAccount || a.trading_account}</td>
                <td className={`status-${a.Status || a.status}`}>{statusLabel(a.Status || a.status)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
