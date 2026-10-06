export type AdapterLike = {
  id?: string;
  ID?: string;
  name?: string;
  Name?: string;
  type?: string;
  Type?: string;
  host?: string;
  Host?: string;
  port?: number;
  Port?: number;
  status?: string;
  Status?: string;
  weight?: number;
  Weight?: number;
  active_orders?: number;
  ActiveOrders?: number;
  processed_orders?: number;
  ProcessedOrders?: number;
  error_count?: number;
  ErrorCount?: number;
  enabled?: boolean;
  Enabled?: boolean;
};

export type NormalizedAdapter = {
  id: string;
  name: string;
  type: string;
  host: string;
  port: number | string;
  status: string;
  weight: number;
  active_orders: number;
  processed_orders: number;
  error_count: number;
  enabled: boolean;
};

export function normalizeAdapter(a: AdapterLike): NormalizedAdapter {
  return {
    id: String(a.id ?? a.ID ?? ""),
    name: String(a.name ?? a.Name ?? ""),
    type: String(a.type ?? a.Type ?? ""),
    host: String(a.host ?? a.Host ?? ""),
    port: a.port ?? a.Port ?? "",
    status: String(a.status ?? a.Status ?? ""),
    weight: Number(a.weight ?? a.Weight ?? 0),
    active_orders: Number(a.active_orders ?? a.ActiveOrders ?? 0),
    processed_orders: Number(a.processed_orders ?? a.ProcessedOrders ?? 0),
    error_count: Number(a.error_count ?? a.ErrorCount ?? 0),
    enabled: a.enabled ?? a.Enabled ?? true,
  };
}

export function isSimulator(a: AdapterLike): boolean {
  const n = normalizeAdapter(a);
  const type = n.type.toLowerCase();
  const id = n.id.toLowerCase();
  const name = n.name.toLowerCase();
  return type === "simulator" || id.includes("simulator") || name.includes("simulator");
}

export function isHiddenAdapter(a: AdapterLike): boolean {
  return !normalizeAdapter(a).enabled;
}

export function displayAdapterName(name: string): string {
  return name.replace(/\bprimary\b/gi, "основной");
}
