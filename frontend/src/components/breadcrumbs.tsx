import { createContext, useContext, useEffect, useMemo, useState } from "react";

type Crumb = { label: string; to?: string };

const BreadcrumbCtx = createContext<{
  crumbs: Crumb[] | null;
  setCrumbs: (c: Crumb[] | null) => void;
}>({ crumbs: null, setCrumbs: () => {} });

export function BreadcrumbProvider({ children }: { children: React.ReactNode }) {
  const [crumbs, setCrumbs] = useState<Crumb[] | null>(null);
  const value = useMemo(() => ({ crumbs, setCrumbs }), [crumbs]);
  return <BreadcrumbCtx.Provider value={value}>{children}</BreadcrumbCtx.Provider>;
}

export function useBreadcrumbs(crumbs: Crumb[] | null) {
  const { setCrumbs } = useContext(BreadcrumbCtx);
  const serialized = crumbs ? JSON.stringify(crumbs) : "";
  useEffect(() => {
    if (!serialized) {
      setCrumbs(null);
      return () => setCrumbs(null);
    }
    let parsed: Crumb[] = [];
    try {
      parsed = JSON.parse(serialized) as Crumb[];
    } catch {
      parsed = [];
    }
    setCrumbs(parsed);
    return () => setCrumbs(null);
  }, [serialized, setCrumbs]);
}

export function useBreadcrumbState() {
  return useContext(BreadcrumbCtx);
}
