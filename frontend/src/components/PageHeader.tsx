import type { ReactNode } from "react";

export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="page-heading flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <h1 className="font-semibold tracking-[-0.025em] text-ink">{title}</h1>
        {subtitle ? <p className="mt-1 text-[13px] leading-snug text-muted">{subtitle}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}
