import type { ReactNode } from "react";

export function PageSpinner({ label = "Загрузка…" }: { label?: string }) {
  return (
    <div className="flex items-center justify-center py-16 text-sm text-muted" role="status">
      {label}
    </div>
  );
}

export function TableSkeleton({ rows = 6, cols = 6 }: { rows?: number; cols?: number }) {
  return (
    <div className="animate-pulse space-y-2 p-4" aria-hidden>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex gap-3">
          {Array.from({ length: cols }).map((__, j) => (
            <div key={j} className="h-3 flex-1 rounded bg-line/80" />
          ))}
        </div>
      ))}
    </div>
  );
}

export function EmptyState({ title, hint }: { title: string; hint?: ReactNode }) {
  return (
    <div className="px-4 py-12 text-center">
      <div className="text-sm font-medium text-ink/80">{title}</div>
      {hint ? <div className="mt-1 text-sm text-muted">{hint}</div> : null}
    </div>
  );
}

export function ErrorBanner({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message = error instanceof Error ? error.message : "Не удалось загрузить данные";
  return (
    <div className="card flex flex-wrap items-center justify-between gap-3 border-rose-100 bg-rose-50 text-sm text-danger" role="alert">
      <span>{message}</span>
      {onRetry ? (
        <button type="button" className="btn-muted" onClick={onRetry}>
          Повторить
        </button>
      ) : null}
    </div>
  );
}
