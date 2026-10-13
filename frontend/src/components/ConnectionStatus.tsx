import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { useAdminStream } from "../hooks/useAdminStream";
import { connectionSummary } from "../lib/connections";
import { displayAdapterName } from "../lib/adapters";
import type { Adapter } from "../types/admin";
import { Icons } from "./icons";

export function ConnectionStatus() {
  const { connected } = useAdminStream();
  const [now, setNow] = useState(Date.now());
  const query = useQuery({ queryKey: ["adapters"], queryFn: ({ signal }) => api<{ adapters: Adapter[] }>("/api/v1/admin/adapters", { signal: AbortSignal.any([signal, AbortSignal.timeout(5000)]) }), refetchInterval: 5000, retry: false });
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 5000); return () => window.clearInterval(timer); }, []);
  const stale = !query.dataUpdatedAt || now - query.dataUpdatedAt > 15000;
  const status = connectionSummary(query.data?.adapters ?? [], !!query.error || stale);
  return <footer className="connection-bar" aria-label="Состояние подключений">
    <Link to="/adapters" className={`connection-main connection-${status.state}`} title="Открыть подключения FIX / QUIK"><Icons.adapters size={15} /><strong>QUIK / FIX</strong><span className="connection-dot" /><span role="status">{query.isLoading ? "Проверка соединений…" : status.label}</span>{status.quik.length > 0 && <span className="connection-count">{status.online}/{status.quik.length}</span>}</Link>
    <div className="connection-adapters">{status.quik.map(a => <Link to="/adapters" key={a.id} className={`connection-adapter connection-${status.state === "unknown" ? "unknown" : a.status.toLowerCase()}`} title={`${a.id} · ${a.host}:${a.port} · ${stale || query.error ? "Последнее известное состояние" : a.status}`}><span className="connection-dot" />{displayAdapterName(a.name || a.id)}</Link>)}{status.simulators.length > 0 && <Link to="/adapters" className="connection-simulator">Тестовый контур · {status.simulators.length}</Link>}</div>
    <div className="connection-meta"><span className={connected ? "text-emerald-700" : "text-muted"}><span className="connection-dot" />{connected ? "События live" : "Резервный опрос"}</span><span>{query.dataUpdatedAt ? `Проверка ${new Date(query.dataUpdatedAt).toLocaleTimeString("ru-RU")}` : "Ожидание данных"}</span></div>
  </footer>;
}
