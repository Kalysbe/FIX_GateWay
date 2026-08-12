import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { statusLabel } from "../labels";

type Broker = { id: number; code: string; name: string; description: string; status: string; created_at: string };

export default function BrokersPage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["brokers"], queryFn: () => api<{ brokers: Broker[] }>("/api/v1/admin/brokers") });
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => api("/api/v1/admin/brokers", { method: "POST", body: JSON.stringify({ code, name, description: "" }) }),
    onSuccess: () => {
      setCode("");
      setName("");
      qc.invalidateQueries({ queryKey: ["brokers"] });
    },
  });

  async function setStatus(id: number, status: string) {
    await api(`/api/v1/admin/brokers/${id}`, { method: "PATCH", body: JSON.stringify({ status }) });
    qc.invalidateQueries({ queryKey: ["brokers"] });
  }

  function onCreate(e: FormEvent) {
    e.preventDefault();
    create.mutate();
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Брокеры</h1>
        <p className="text-sm text-slate-500">Управление брокерскими компаниями и доступом</p>
      </div>
      <form onSubmit={onCreate} className="card grid gap-3 md:grid-cols-4">
        <input className="input" placeholder="Код" value={code} onChange={(e) => setCode(e.target.value)} required />
        <input className="input" placeholder="Название" value={name} onChange={(e) => setName(e.target.value)} required />
        <button className="btn" type="submit">
          Создать
        </button>
      </form>
      <div className="card overflow-x-auto">
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Код</th>
              <th>Название</th>
              <th>Статус</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {(q.data?.brokers || []).map((b) => (
              <tr key={b.id}>
                <td>{b.id}</td>
                <td className="font-mono">{b.code}</td>
                <td>{b.name}</td>
                <td className={`status-${b.status}`}>{statusLabel(b.status)}</td>
                <td className="space-x-2">
                  <Link className="btn-muted" to={`/brokers/${b.id}`}>
                    Открыть
                  </Link>
                  <Link className="btn-muted" to={`/brokers/${b.id}/tokens`}>
                    Токены
                  </Link>
                  {b.status === "ACTIVE" ? (
                    <button className="btn-muted" onClick={() => setStatus(b.id, "BLOCKED")}>
                      Заблокировать
                    </button>
                  ) : (
                    <button className="btn-muted" onClick={() => setStatus(b.id, "ACTIVE")}>
                      Активировать
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
