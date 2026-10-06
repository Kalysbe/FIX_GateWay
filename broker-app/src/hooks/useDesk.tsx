import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ApiError,
  cancelOrder as apiCancel,
  createOrder as apiCreate,
  listOrders,
  mapGatewayOrder,
  probeToken,
} from "../lib/api";
import {
  createId,
  emptyState,
  exportSafeState,
  loadState,
  makeBroker,
  mergeBrokerOrders,
  mergeSettings,
  nowIso,
  saveState,
  updateOrderInList,
} from "../lib/storage";
import type {
  ActivityEntry,
  AppState,
  BrokerProfile,
  CreateOrderInput,
  DeskSettings,
  LocalOrder,
} from "../types";
import { PollGate } from "../lib/pollGate";

interface DeskApi {
  state: AppState;
  activeBroker: BrokerProfile | null;
  connectedCount: number;
  setActiveBrokerId: (id: string | null) => void;
  addBroker: (partial?: Partial<BrokerProfile>) => void;
  updateBroker: (id: string, patch: Partial<BrokerProfile>) => void;
  removeBroker: (id: string) => void;
  connect: (id: string) => Promise<void>;
  disconnect: (id: string) => void;
  connectAll: () => Promise<void>;
  disconnectAll: () => void;
  placeOrder: (brokerId: string, input: CreateOrderInput) => Promise<LocalOrder | null>;
  cancelOrder: (orderId: string) => Promise<void>;
  refreshOrders: (brokerId?: string) => Promise<void>;
  updateSettings: (patch: Partial<DeskSettings>) => void;
  clearOrders: () => void;
  clearLog: () => void;
  resetAll: () => void;
  exportJson: () => string;
  importJson: (raw: string) => void;
}

const DeskContext = createContext<DeskApi | null>(null);

function logEntry(
  level: ActivityEntry["level"],
  message: string,
  brokerId?: string,
): ActivityEntry {
  return { id: createId("log"), at: nowIso(), level, message, brokerId };
}

export function DeskProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AppState>(() => loadState());
  const stateRef = useRef(state);
  const connectionAttempts = useRef(new Map<string, symbol>());
  const pollGate = useRef(new PollGate());
  stateRef.current = state;

  useEffect(() => {
    saveState(state);
  }, [state]);

  const pushLog = useCallback((entry: ActivityEntry) => {
    setState((s) => ({ ...s, activityLog: [entry, ...s.activityLog].slice(0, 200) }));
  }, []);

  const setActiveBrokerId = useCallback((id: string | null) => {
    setState((s) => ({ ...s, activeBrokerId: id }));
  }, []);

  const updateBroker = useCallback((id: string, patch: Partial<BrokerProfile>) => {
    if (patch.apiToken !== undefined) connectionAttempts.current.delete(id);
    setState((s) => ({
      ...s,
      brokers: s.brokers.map((b) => (b.id === id ? {
        ...b, ...patch,
        ...(patch.apiToken !== undefined && patch.apiToken !== b.apiToken
          ? { connected: false, connectionStatus: "DISCONNECTED" as const, connectedAt: undefined }
          : {}),
      } : b)),
    }));
  }, []);

  const addBroker = useCallback((partial?: Partial<BrokerProfile>) => {
    const broker = makeBroker(partial, stateRef.current.brokers.length);
    setState((s) => ({
      ...s,
      brokers: [...s.brokers, broker],
      activeBrokerId: broker.id,
      activityLog: [
        logEntry("info", `Сессия «${broker.name}» создана — вставьте API-токен и нажмите Connect`, broker.id),
        ...s.activityLog,
      ],
    }));
  }, []);

  const removeBroker = useCallback((id: string) => {
    connectionAttempts.current.delete(id);
    pollGate.current.success(id);
    setState((s) => ({
      ...s,
      brokers: s.brokers.filter((b) => b.id !== id),
      orders: s.orders.filter((o) => o.brokerId !== id),
      activeBrokerId:
        s.activeBrokerId === id ? s.brokers.find((b) => b.id !== id)?.id ?? null : s.activeBrokerId,
      activityLog: [logEntry("warn", "Сессия удалена", id), ...s.activityLog],
    }));
  }, []);

  const syncBrokerOrders = useCallback(async (broker: BrokerProfile) => {
    const gatewayUrl = stateRef.current.settings.gatewayUrl;
    const attempt = connectionAttempts.current.get(broker.id);
    const remote = await listOrders(gatewayUrl, broker.apiToken);
    const mapped = remote.map((o) => mapGatewayOrder(broker.id, o));
    setState((s) => {
      const current = s.brokers.find((b) => b.id === broker.id);
      if (!current?.connected || current.apiToken !== broker.apiToken ||
          s.settings.gatewayUrl !== gatewayUrl ||
          connectionAttempts.current.get(broker.id) !== attempt) return s;
      return {
      ...s,
      orders: mergeBrokerOrders(s.orders, broker.id, mapped),
      };
    });
  }, []);

  const connect = useCallback(
    async (id: string) => {
      const broker = stateRef.current.brokers.find((b) => b.id === id);
      if (!broker) return;
      const token = broker.apiToken.trim();
      pollGate.current.success(id);
      const attempt = Symbol();
      connectionAttempts.current.set(id, attempt);
      if (!token) {
        updateBroker(id, {
          connectionStatus: "ERROR",
          connected: false,
          lastError: "Вставьте API-токен из админки",
        });
        pushLog(logEntry("error", `${broker.name}: нет токена`, id));
        return;
      }

      updateBroker(id, { connected: false, connectionStatus: "CONNECTING", lastError: undefined });
      pushLog(logEntry("info", `${broker.name}: проверка токена на gateway…`, id));

      try {
        const { orders, accounts } = await probeToken(stateRef.current.settings.gatewayUrl, token);
        if (connectionAttempts.current.get(id) !== attempt) return;
        const firstAcc = accounts[0];
        const accountHint = firstAcc?.trading_account || broker.accountId;

        setState((s) => {
          if (connectionAttempts.current.get(id) !== attempt || !s.brokers.some((b) => b.id === id)) return s;
          const patched = s.brokers.map((b) =>
            b.id === id
              ? {
                  ...b,
                  connected: true,
                  connectionStatus: "CONNECTED" as const,
                  connectedAt: nowIso(),
                  lastError: undefined,
                  accountId: b.accountId || accountHint || "",
                }
              : b,
          );
          const mapped = orders.map((o) => mapGatewayOrder(id, o));
          return {
            ...s,
            brokers: patched,
            orders: mergeBrokerOrders(s.orders, id, mapped),
            activityLog: [
              logEntry("ok", `${broker.name}: CONNECTED · ${orders.length} заявок с gateway`, id),
              ...s.activityLog,
            ],
          };
        });
      } catch (e) {
        if (connectionAttempts.current.get(id) !== attempt) return;
        const msg = e instanceof ApiError ? `${e.status}: ${e.message}` : e instanceof Error ? e.message : "connect failed";
        updateBroker(id, { connectionStatus: "ERROR", connected: false, lastError: msg });
        pushLog(logEntry("error", `${broker.name}: ${msg}`, id));
      }
    },
    [pushLog, updateBroker],
  );

  const disconnect = useCallback((id: string) => {
    connectionAttempts.current.delete(id);
    setState((s) => {
      const broker = s.brokers.find((b) => b.id === id);
      if (!broker) return s;
      return {
        ...s,
        brokers: s.brokers.map((b) =>
          b.id === id
            ? {
                ...b,
                connected: false,
                connectionStatus: "DISCONNECTED" as const,
                connectedAt: undefined,
              }
            : b,
        ),
        activityLog: [logEntry("warn", `${broker.name}: DISCONNECTED`, id), ...s.activityLog],
      };
    });
  }, []);

  const connectAll = useCallback(async () => {
    const ids = stateRef.current.brokers.map((b) => b.id);
    await Promise.all(ids.map((id) => connect(id)));
  }, [connect]);

  const disconnectAll = useCallback(() => {
    stateRef.current.brokers.forEach((b) => disconnect(b.id));
  }, [disconnect]);

  const refreshOrders = useCallback(
    async (brokerId?: string) => {
      const targets = stateRef.current.brokers.filter(
        (b) => b.connected && b.apiToken.trim() && (!brokerId || b.id === brokerId),
      );
      await Promise.all(
        targets.map(async (b) => {
          const pollKey = b.id;
          if (!pollGate.current.begin(pollKey)) return;
          const attempt = connectionAttempts.current.get(b.id);
          try {
            await syncBrokerOrders(b);
            pollGate.current.success(pollKey);
          } catch (e) {
            if (connectionAttempts.current.get(b.id) !== attempt) return;
            const msg = e instanceof Error ? e.message : "refresh failed";
            pushLog(logEntry("error", `${b.name}: poll ${msg}`, b.id));
            if (e instanceof ApiError && e.status >= 400 && e.status < 500 && e.status !== 429) {
              updateBroker(b.id, {
                connected: false,
                connectionStatus: "ERROR",
                lastError: msg,
              });
            } else {
              pollGate.current.fail(pollKey, e instanceof ApiError ? e.retryAfterMs : 0);
            }
          } finally {
            pollGate.current.finish(pollKey);
          }
        }),
      );
    },
    [pushLog, syncBrokerOrders, updateBroker],
  );

  // Poll connected brokers
  useEffect(() => {
    const ms = Math.max(1000, state.settings.pollIntervalMs || 2000);
    const t = window.setInterval(() => {
      if (stateRef.current.brokers.some((b) => b.connected)) {
        void refreshOrders();
      }
    }, ms);
    return () => window.clearInterval(t);
  }, [state.settings.pollIntervalMs, refreshOrders]);

  const placeOrder = useCallback(
    async (brokerId: string, input: CreateOrderInput): Promise<LocalOrder | null> => {
      const broker = stateRef.current.brokers.find((b) => b.id === brokerId);
      if (!broker) return null;
      if (!broker.connected || !broker.apiToken.trim()) {
        pushLog(logEntry("error", `${broker.name}: сначала Connect с валидным токеном`, brokerId));
        return null;
      }

      const clientOrderId =
        input.clientOrderId?.trim() ||
        `${broker.code}-${Date.now().toString(36).toUpperCase()}-${Math.random().toString(36).slice(2, 6).toUpperCase()}`;

      const payload: CreateOrderInput = {
        ...input,
        clientOrderId,
        accountId: input.accountId || broker.accountId,
        clientCode: input.clientCode || broker.clientCode,
      };

      if (!payload.accountId || !payload.clientCode) {
        pushLog(logEntry("error", `${broker.name}: заполните account_id и client_code`, brokerId));
        return null;
      }

      try {
        const res = await apiCreate(stateRef.current.settings.gatewayUrl, broker.apiToken, payload);
        const order = mapGatewayOrder(brokerId, res.order);
        setState((s) => ({
          ...s,
          orders: updateOrderInList(s.orders, order),
          activityLog: [
            logEntry(
              "ok",
              `${broker.name}: ${res.idempotent ? "idempotent" : "NEW"} ${order.side} ${order.quantity} ${order.symbol} → ${order.status}`,
              brokerId,
            ),
            ...s.activityLog,
          ],
        }));
        return order;
      } catch (e) {
        const msg = e instanceof Error ? e.message : "create failed";
        pushLog(logEntry("error", `${broker.name}: ${msg}`, brokerId));
        return null;
      }
    },
    [pushLog],
  );

  const cancelOrder = useCallback(
    async (orderId: string) => {
      const order = stateRef.current.orders.find((o) => o.id === orderId);
      if (!order) return;
      const broker = stateRef.current.brokers.find((b) => b.id === order.brokerId);
      if (!broker?.connected || !broker.apiToken.trim()) {
        pushLog(logEntry("error", "Брокер не подключён", order.brokerId));
        return;
      }
      try {
        const remote = await apiCancel(stateRef.current.settings.gatewayUrl, broker.apiToken, orderId);
        const mapped = mapGatewayOrder(broker.id, remote);
        setState((s) => ({
          ...s,
          orders: updateOrderInList(s.orders, mapped),
          activityLog: [logEntry("warn", `${broker.name}: cancel ${order.clientOrderId}`, broker.id), ...s.activityLog],
        }));
      } catch (e) {
        const msg = e instanceof Error ? e.message : "cancel failed";
        pushLog(logEntry("error", `${broker.name}: ${msg}`, broker.id));
      }
    },
    [pushLog],
  );

  const updateSettings = useCallback((patch: Partial<DeskSettings>) => {
    const gatewayChanged = patch.gatewayUrl !== undefined && patch.gatewayUrl !== stateRef.current.settings.gatewayUrl;
    if (gatewayChanged) connectionAttempts.current.clear();
    setState((s) => ({
      ...s,
      settings: mergeSettings(s.settings, patch),
      brokers: gatewayChanged ? s.brokers.map((b) => ({
        ...b, connected: false, connectionStatus: "DISCONNECTED" as const, connectedAt: undefined,
      })) : s.brokers,
    }));
  }, []);

  const clearOrders = useCallback(() => {
    setState((s) => ({
      ...s,
      orders: [],
      activityLog: [logEntry("warn", "Локальный список заявок очищен (gateway не тронут)"), ...s.activityLog],
    }));
  }, []);

  const clearLog = useCallback(() => {
    setState((s) => ({ ...s, activityLog: [] }));
  }, []);

  const resetAll = useCallback(() => {
    connectionAttempts.current.clear();
    const next = emptyState();
    setState(next);
    saveState(next);
  }, []);

  const exportJson = useCallback(() => exportSafeState(stateRef.current), []);

  const importJson = useCallback(
    (raw: string) => {
      const parsed = JSON.parse(raw) as AppState;
      connectionAttempts.current.clear();
      setState({
        ...emptyState(),
        ...parsed,
        settings: mergeSettings(emptyState().settings, parsed.settings || {}),
        brokers: (parsed.brokers || []).map((b) => ({
          ...b,
          connected: false,
          connectionStatus: "DISCONNECTED" as const,
        })),
      });
      pushLog(logEntry("ok", "Состояние импортировано — сделайте Connect по токенам"));
    },
    [pushLog],
  );

  const activeBroker = useMemo(
    () => state.brokers.find((b) => b.id === state.activeBrokerId) ?? null,
    [state.brokers, state.activeBrokerId],
  );

  const connectedCount = state.brokers.filter((b) => b.connected).length;

  const api: DeskApi = {
    state,
    activeBroker,
    connectedCount,
    setActiveBrokerId,
    addBroker,
    updateBroker,
    removeBroker,
    connect,
    disconnect,
    connectAll,
    disconnectAll,
    placeOrder,
    cancelOrder,
    refreshOrders,
    updateSettings,
    clearOrders,
    clearLog,
    resetAll,
    exportJson,
    importJson,
  };

  return <DeskContext.Provider value={api}>{children}</DeskContext.Provider>;
}

export function useDesk(): DeskApi {
  const ctx = useContext(DeskContext);
  if (!ctx) throw new Error("useDesk must be used within DeskProvider");
  return ctx;
}
