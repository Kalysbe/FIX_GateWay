import { isSimulator, normalizeAdapter, type AdapterLike } from "./adapters";

export function connectionSummary(adapters: AdapterLike[], unavailable = false) {
  const normalized = adapters.map(normalizeAdapter);
  const quik = normalized.filter(a => a.enabled && !isSimulator(a));
  const simulators = normalized.filter(a => a.enabled && isSimulator(a));
  const online = quik.filter(a => a.status.toUpperCase() === "ONLINE").length;
  const degraded = quik.some(a => a.status.toUpperCase() === "DEGRADED");
  const state = unavailable ? "unknown" : !quik.length ? "empty" : online === quik.length ? "online" : online > 0 || degraded ? "degraded" : "offline";
  const label = ({ unknown: "Статус недоступен", empty: "Не настроен", online: "Подключён", degraded: "Частично доступен", offline: "Нет соединения" })[state];
  return { quik, simulators, online, state, label, disabled: normalized.filter(a => !a.enabled).length };
}
