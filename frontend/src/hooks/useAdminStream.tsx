import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { isSessionValid, consumeAdminEvents, redirectToLogin } from "../api";

import { eventRefresh } from "../lib/eventRefresh";

export type AdminStreamMsg = { type?: string; data?: unknown };

type AdminStreamValue = {
  connected: boolean;
  subscribe: (fn: (msg: AdminStreamMsg) => void) => () => void;
};

const Ctx = createContext<AdminStreamValue>({
  connected: false,
  subscribe: () => () => {},
});

export function useAdminStream() {
  return useContext(Ctx);
}

export function AdminStreamProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const [connected, setConnected] = useState(false);
  const listeners = useRef(new Set<(msg: AdminStreamMsg) => void>());

  const subscribe = useCallback((fn: (msg: AdminStreamMsg) => void) => {
    listeners.current.add(fn);
    return () => {
      listeners.current.delete(fn);
    };
  }, []);

  useEffect(() => {
    if (!isSessionValid()) return;
    let closed = false;
    let active: AbortController | null = null;
    let retry: number | undefined;
    let delay = 1000;

    const refresh = eventRefresh(keys => {
      for (const key of keys) void qc.invalidateQueries({ queryKey: key }, { cancelRefetch: false });
    });

    const onMessage = (msg: AdminStreamMsg) => {
      listeners.current.forEach((fn) => fn(msg));
      switch (msg.type) {
        case "notification.new":
          refresh.add([["notifications"]]);
          break;
        case "order.updated":
        case "trade.created":
          refresh.add([["admin-orders"]]);
          refresh.add([["order"]]);
          refresh.add([["order-trace"]]);
          refresh.add([["admin-trades"]]);
          refresh.add([["dashboard"]]);
          break;
        case "positions.updated":
          refresh.add([["admin-positions"]]);
          break;
        case "adapter.status_changed":
          refresh.add([["adapters"]]);
          refresh.add([["dashboard"]]);
          break;
        case "obs.event":
          refresh.add([["dashboard"]]);
          refresh.add([["logs"], ["broker-logs"], ["broker-errors"]]);
          break;
        default:
          break;
      }
    };

    const connect = () => {
      if (closed || active || document.visibilityState === "hidden") return;
      const controller = new AbortController();
      active = controller;
      void consumeAdminEvents(controller.signal, () => {
        if (active !== controller || closed) return;
        delay = 1000;
        setConnected(true);
        void qc.invalidateQueries({}, { cancelRefetch: false }); // Refresh events missed during reconnect.
      }, onMessage).catch(() => {}).finally(() => {
        if (active !== controller || closed) return;
        active = null;
        setConnected(false);
        if (document.visibilityState === "hidden") return;
        if (!isSessionValid()) { redirectToLogin(); return; }
        retry = window.setTimeout(connect, delay);
        delay = Math.min(delay * 2, 15_000);
      });
    };
    const onVis = () => {
      if (document.visibilityState === "hidden") {
        if (retry) window.clearTimeout(retry);
        active?.abort(); active = null;
        setConnected(false);
        return;
      }
      connect();
    };

    document.addEventListener("visibilitychange", onVis);
    connect();
    return () => {
      closed = true;
      document.removeEventListener("visibilitychange", onVis);
      if (retry) window.clearTimeout(retry);
      refresh.stop();
      active?.abort(); active = null;
    };
  }, [qc]);

  const value = useMemo(() => ({ connected, subscribe }), [connected, subscribe]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
