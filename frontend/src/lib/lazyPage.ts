import { lazy, type ComponentType } from "react";

const RELOAD_KEY = "fixgw-chunk-reload";

/**
 * A route page loaded on demand. After a new release the old chunk names are
 * gone from the server; a page still open on the old build then reloads once
 * to pick up the new one instead of showing a broken screen.
 */
export function lazyPage<T extends ComponentType<object>>(load: () => Promise<{ default: T }>) {
  return lazy(async () => {
    try {
      const page = await load();
      try { sessionStorage.removeItem(RELOAD_KEY); } catch { /* storage may be blocked */ }
      return page;
    } catch (error) {
      let reloaded = true;
      try {
        reloaded = sessionStorage.getItem(RELOAD_KEY) === "1";
        if (!reloaded) sessionStorage.setItem(RELOAD_KEY, "1");
      } catch { /* storage may be blocked */ }
      if (!reloaded) {
        window.location.reload();
        return new Promise<never>(() => {});
      }
      throw error;
    }
  });
}
