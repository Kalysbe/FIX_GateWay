import { statusLabel } from "../labels";

const TONE: Record<string, string> = {
  ACTIVE: "badge-success",
  CONNECTED: "badge-success",
  ONLINE: "badge-success",
  FILLED: "badge-success",
  PARTIALLY_FILLED: "badge-info",
  ACKNOWLEDGED: "badge-info",
  REVOKED: "badge-danger",
  REJECTED: "badge-danger",
  FAILED: "badge-danger",
  BLOCKED: "badge-danger",
  DISABLED: "badge-muted",
  OFFLINE: "badge-danger",
  EXPIRED: "badge-warn",
  QUEUED: "badge-warn",
  CANCELLED: "badge-muted",
  CANCELED: "badge-muted",
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
  return <span className={`status-badge ${TONE[key] || "badge-muted"}`} title={status}><i aria-hidden="true" />{statusLabel(status)}</span>;
}
