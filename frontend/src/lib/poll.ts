export function liveRefetchInterval(connected: boolean, fallbackMs: number, liveMs = 30_000) {
  return () => {
    if (typeof document !== "undefined" && document.visibilityState === "hidden") return false;
    return connected ? liveMs : fallbackMs;
  };
}

export const ROLE_LABEL: Record<string, string> = {
  SUPER_ADMIN: "Суперадминистратор",
  ADMIN: "Администратор",
  OPERATOR: "Оператор",
  AUDITOR: "Аудитор",
  READ_ONLY: "Только чтение",
};

export function roleLabel(role?: string | null): string {
  if (!role) return "Администратор";
  return ROLE_LABEL[role.toUpperCase()] || role;
}

export function userInitials(name?: string, email?: string): string {
  const src = (name || email || "A").trim();
  const parts = src.split(/[\s@._-]+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return src.slice(0, 2).toUpperCase();
}
