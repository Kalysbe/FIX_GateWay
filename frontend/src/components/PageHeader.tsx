import type { ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { navItemFor } from "../lib/navigation";

export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
}) {
  // The page's menu item gives its section hue to what the page tints (tabs, accents).
  const item = navItemFor(useLocation().pathname);
  return (
    <div className="page-heading flex flex-wrap items-end justify-between gap-3" data-section={item?.section}>
      <div className="flex min-w-0 items-center gap-3">
        <div className="min-w-0">
          <h1 className="font-semibold tracking-[-0.025em] text-ink">{title}</h1>
          {subtitle ? <p className="mt-1 text-[13px] leading-snug text-muted">{subtitle}</p> : null}
        </div>
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}
