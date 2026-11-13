import type { ReactNode } from "react";
import { Icons } from "./icons";

export function PageSpinner({ label = "Загрузка…" }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-2.5 py-16 text-[13px] text-muted" role="status">
      <span className="h-4 w-4 animate-spin rounded-full border-2 border-line border-t-accent" aria-hidden />
      {label}
    </div>
  );
}

export function TableSkeleton({ rows = 6, cols = 6 }: { rows?: number; cols?: number }) {
  return (
    <div className="animate-pulse space-y-3 p-4" aria-hidden>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex gap-3">
          {Array.from({ length: cols }).map((__, j) => (
            <div key={j} className="h-3 flex-1 rounded-full bg-subtle" />
          ))}
        </div>
      ))}
    </div>
  );
}

export function EmptyState({ title, hint }: { title: string; hint?: ReactNode }) {
  return (
    <div className="flex flex-col items-center px-4 py-10 text-center">
      <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-subtle text-faint" aria-hidden>
        <Icons.inbox size={18} />
      </div>
      <div className="text-[13px] font-medium text-ink">{title}</div>
      {hint ? <div className="mt-1 max-w-md text-[12px] text-muted">{hint}</div> : null}
    </div>
  );
}

export function ErrorBanner({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message = error instanceof Error ? error.message : "Не удалось загрузить данные";
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-danger/25 bg-danger-soft px-4 py-3 text-[13px] text-danger" role="alert">
      <span className="flex min-w-0 items-start gap-2.5">
        <Icons.info size={16} className="mt-0.5 shrink-0" />
        <span className="min-w-0 break-words">{message}</span>
      </span>
      {onRetry ? (
        <button type="button" className="btn-muted h-8" onClick={onRetry}>
          <Icons.refresh size={14} />
          Повторить
        </button>
      ) : null}
    </div>
  );
}
