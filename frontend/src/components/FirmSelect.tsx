import { useQuery } from "@tanstack/react-query";
import { apiWithHeaders } from "../api";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";

// Token firms (FIX tag 5018) are chosen from QUIK "Firms"; the server refuses
// any other value.
export function FirmSelect({ value, onChange, label = "Код фирмы QUIK (FIX 5018)" }: { value: string; onChange: (v: string) => void; label?: string }) {
  const firms = useQuery({
    queryKey: ["admin-firms"],
    queryFn: async ({ signal }) => (await apiWithHeaders<DBOrder[]>("/api/v1/admin/firms", { signal }, parseDBJSON)).data ?? [],
    staleTime: 60_000,
  });
  const ids = Array.from(new Set((firms.data ?? []).map(f => dbValue(f.FirmId).trim()).filter(id => id && id !== "null")));
  const names = new Map((firms.data ?? []).map(f => [dbValue(f.FirmId).trim(), f.FirmName == null ? "" : dbValue(f.FirmName).trim()]));
  return <label className="mb-3 block space-y-1 text-sm">
    <span className="text-slate-500">{label}</span>
    <select className="input font-mono" aria-label={label} value={value} onChange={e => onChange(e.target.value)} disabled={firms.isLoading || !!firms.error} required>
      <option value="">{firms.isLoading ? "Загрузка фирм QUIK…" : "Выберите фирму"}</option>
      {value && !ids.includes(value) && <option value={value}>{value} (нет в QUIK Firms)</option>}
      {ids.map(id => <option key={id} value={id}>{names.get(id) ? `${id} · ${names.get(id)}` : id}</option>)}
    </select>
    {firms.error && <span className="block text-xs text-danger">Таблица QUIK Firms недоступна: {(firms.error as Error).message}</span>}
  </label>;
}
