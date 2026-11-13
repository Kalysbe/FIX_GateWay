import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

/**
 * A string kept in the page URL, so a reload, the Back button or a shared link
 * restores it. The default value is left out of the URL; changes replace the
 * history entry instead of adding one per keystroke.
 */
export function useUrlState(name: string, fallback = ""): [string, (value: string) => void] {
  const [params, setParams] = useSearchParams();
  const value = params.get(name) ?? fallback;
  const set = useCallback((next: string) => {
    setParams(prev => {
      const out = new URLSearchParams(prev);
      if (next === fallback || next === "") out.delete(name);
      else out.set(name, next);
      return out;
    }, { replace: true });
  }, [name, fallback, setParams]);
  return [value, set];
}

/** Several URL values changed at once (one history entry). */
export function useUrlPatch() {
  const [, setParams] = useSearchParams();
  return useCallback((patch: Record<string, string | null>) => {
    setParams(prev => {
      const out = new URLSearchParams(prev);
      for (const [k, v] of Object.entries(patch)) {
        if (v == null || v === "") out.delete(k);
        else out.set(k, v);
      }
      return out;
    }, { replace: true });
  }, [setParams]);
}
