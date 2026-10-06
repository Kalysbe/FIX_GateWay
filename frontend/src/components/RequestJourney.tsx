type JourneyEvent = { id: string; timestamp: string; type: string; subtype: string; message?: string; adapter_id?: string; status_code?: number };

// Only recorded events become arrows; missing hops are never inferred as successes.
function hop(e: JourneyEvent): [number, number, string] | null {
  if (e.type === "API_REQUEST") return [1, 0, `HTTP-ответ ${e.status_code ?? ""}`];
  if (e.subtype === "ORDER_CREATED") return [0, 1, "Заявка принята шлюзом"];
  if (e.subtype === "ORDER_ROUTED" || e.subtype === "ORDER_ASSIGNED") return [1, 2, "Выбран адаптер"];
  if (e.subtype === "FIX_ORDER_SENT") return [2, 3, "FIX отправлен"];
  if (e.subtype === "FIX_EXECUTION_REPORT") return [3, 2, "ExecutionReport получен"];
  if (e.subtype === "FIX_REJECT") return [3, 2, "FIX-отказ получен"];
  if (["ORDER_ACKNOWLEDGED", "ORDER_PARTIALLY_FILLED", "ORDER_FILLED", "ORDER_CANCELLED", "ORDER_REPLACED"].includes(e.subtype)) return [2, 1, e.subtype];
  return null;
}

export function RequestJourney({ events }: { events: JourneyEvent[] }) {
  const sorted = [...events].sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp));
  const arrows = sorted.flatMap(e => { const route = hop(e); return route ? [{ event: e, route }] : []; });
  const baseline = sorted.length ? Date.parse(sorted[0].timestamp) : 0;
  return <div className="card">
    <h2 className="font-semibold">Путь этой заявки</h2>
    <p className="mt-1 text-xs text-muted">Стрелки соответствуют записанным событиям. Время отсчитывается от первого события трассировки.</p>
    {arrows.length === 0 ? <div className="py-6 text-sm text-muted">События маршрута пока не записаны.</div> : <div className="mt-4 max-h-[520px] overflow-auto">
      <svg viewBox={`0 0 800 ${70 + arrows.length * 54}`} className="min-w-[680px] w-full" role="img" aria-label="Последовательность обмена заявки">
        {["Брокер", "Шлюз", "Адаптер", "Биржа / QUIK"].map((label, i) => <g key={label}>
          <text x={100 + i * 200} y="20" textAnchor="middle" fontSize="14" fill="#334155">{label}</text>
          <line x1={100 + i * 200} x2={100 + i * 200} y1="35" y2={65 + arrows.length * 54} stroke="#cbd5e1" strokeDasharray="4 4" />
        </g>)}
        {arrows.map(({ event: e, route: [from, to, label] }, i) => {
          const x1 = 100 + from * 200, x2 = 100 + to * 200, y = 68 + i * 54;
          const color = e.subtype === "FIX_REJECT" ? "#e11d48" : to > from ? "#0284c7" : "#059669";
          return <g key={e.id}>
            <title>{e.timestamp} · {e.subtype} · {e.adapter_id ?? ""} · {e.message ?? ""}</title>
            <line x1={x1} x2={x2} y1={y} y2={y} stroke={color} strokeWidth="2" />
            <path d={`M${x2 + (to > from ? -8 : 8)},${y - 5} L${x2},${y} L${x2 + (to > from ? -8 : 8)},${y + 5}`} fill="none" stroke={color} strokeWidth="2" />
            <text x={(x1 + x2) / 2} y={y - 10} textAnchor="middle" fontSize="11" fill="#334155">{label}</text>
            <text x={(x1 + x2) / 2} y={y + 17} textAnchor="middle" fontSize="10" fill="#64748b">+{Math.max(0, Date.parse(e.timestamp) - baseline)} мс</text>
          </g>;
        })}
      </svg>
    </div>}
    <p className="mt-3 text-xs text-muted">HTTP-ответ фиксирует завершение запроса на шлюзе. Получение брокером последующих событий gRPC отдельно не подтверждено. FIX-отчёт отражает ответ подключённой FIX-системы.</p>
  </div>;
}
