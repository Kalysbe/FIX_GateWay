import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, apiWithHeaders, getSessionUser } from "../api";
import { useToast } from "./Toast";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";

type Settings = { classes: string[]; default_classes: string[] };

const WRITE_ROLES = new Set(["SUPER_ADMIN", "ADMIN", "OPERATOR"]);

export function canEditClasses(role?: string | null): boolean {
  return WRITE_ROLES.has((role || "").toUpperCase());
}

// Classes offered for choice: QUIK Classes plus any chosen code QUIK no longer lists.
export function classOptions(rows: DBOrder[], chosen: string[]): { code: string; name: string }[] {
  const out = rows.filter(r => r.ClassCode != null).map(r => ({ code: dbValue(r.ClassCode).trim(), name: r.ClassName == null ? "" : dbValue(r.ClassName) }));
  for (const code of chosen) if (!out.some(o => o.code === code)) out.push({ code, name: "нет в QUIK Classes" });
  return out;
}

/** The QUIK classes whose current trades are served to the admin UI and brokers. */
export function MarketClassesSettings() {
  const qc = useQueryClient();
  const toast = useToast();
  const canWrite = canEditClasses(getSessionUser()?.role);
  const settingsQ = useQuery({
    queryKey: ["market-data-settings"],
    queryFn: ({ signal }) => api<Settings>("/api/v1/admin/market-data/settings", { signal }),
  });
  const classesQ = useQuery({
    queryKey: ["quik-classes"],
    queryFn: async ({ signal }) => (await apiWithHeaders<DBOrder[]>("/api/v1/admin/classes", { signal }, parseDBJSON)).data ?? [],
  });
  const [draft, setDraft] = useState<string[] | null>(null);
  const saved = settingsQ.data?.classes ?? [];
  const chosen = draft ?? saved;
  useEffect(() => { setDraft(null); }, [settingsQ.data]);

  const save = useMutation({
    mutationFn: () => api<Settings>("/api/v1/admin/market-data/settings", { method: "PATCH", body: JSON.stringify({ classes: chosen }) }),
    onSuccess: () => {
      toast.push("Классы текущих торгов сохранены", "ok");
      qc.invalidateQueries({ queryKey: ["market-data-settings"] });
      qc.invalidateQueries({ queryKey: ["admin-market-data"] });
    },
    onError: (err) => toast.push(err instanceof Error ? err.message : "Не удалось сохранить классы"),
  });

  const toggle = (code: string) => setDraft(chosen.includes(code) ? chosen.filter(c => c !== code) : [...chosen, code]);
  const options = classOptions(classesQ.data ?? [], saved);
  const changed = draft != null && draft.join(",") !== saved.join(",");

  return <div className="card space-y-3">
    <div className="text-[12px] font-medium text-muted">Классы текущих торгов{settingsQ.data ? `: ${saved.join(", ")}` : ""}</div>
    {classesQ.error ? <div className="text-xs text-muted">Таблица QUIK Classes недоступна</div> : null}
    <div className="flex flex-wrap gap-x-4 gap-y-2">
      {options.map(o => <label key={o.code} className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={chosen.includes(o.code)} disabled={!canWrite || save.isPending} onChange={() => toggle(o.code)} />
        <span className="font-mono">{o.code}</span>{o.name && <span className="text-muted">{o.name}</span>}
      </label>)}
    </div>
    {canWrite && <div className="flex gap-2">
      <button className="btn" type="button" disabled={!changed || chosen.length === 0 || save.isPending} onClick={() => save.mutate()}>{save.isPending ? "Сохранение…" : "Сохранить"}</button>
      {changed && <button className="btn-muted" type="button" onClick={() => setDraft(null)}>Отменить</button>}
    </div>}
  </div>;
}
