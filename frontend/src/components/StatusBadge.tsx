import { statusLabel } from "../labels";

const TONE: Record<string, string> = {
  ACTIVE: "badge-success",
  CONNECTED: "badge-success",
  ONLINE: "badge-success",
  FILLED: "badge-success",
  PARTIALLY_FILLED: "badge-success",
  ACKNOWLEDGED: "badge-info",
  REVOKED: "badge-danger",
  REJECTED: "badge-danger",
  FAILED: "badge-danger",
  BLOCKED: "badge-danger",
  OFFLINE: "badge-danger",
  EXPIRED: "badge-warn",
  QUEUED: "badge-warn",
  SENT: "badge-warn",
  ROUTING: "badge-warn",
  DEGRADED: "badge-warn",
  CRITICAL: "badge-danger",
  ERROR: "badge-danger",
  WARNING: "badge-warn",
  INFO: "badge-info",
};

export function StatusBadge({ status }: { status?: string | null }) {
  if (!status) return <span className="badge-muted">—</span>;
  const key = status.toUpperCase();
  return <span className={TONE[key] || "badge-muted"}>{statusLabel(status)}</span>;
}
