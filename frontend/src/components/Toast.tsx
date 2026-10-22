import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

type ToastItem = { id: number; message: string; tone: "ok" | "error" };

type ToastCtx = {
  push: (message: string, tone?: "ok" | "error") => void;
};

const Ctx = createContext<ToastCtx>({ push: () => {} });

export function useToast() {
  return useContext(Ctx);
}

function ToastMessage({ item, onRemove }: { item: ToastItem; onRemove: (id: number) => void }) {
  const [leaving, setLeaving] = useState(false);
  useEffect(() => {
    const timeout = window.setTimeout(() => setLeaving(true), 4500);
    return () => window.clearTimeout(timeout);
  }, []);
  useEffect(() => {
    if (!leaving) return;
    // Fallback for browsers without transitions, hidden tabs and reduced motion.
    const timeout = window.setTimeout(() => onRemove(item.id), 300);
    return () => window.clearTimeout(timeout);
  }, [leaving, item.id, onRemove]);
  return (
    <div
      role="status"
      data-leaving={leaving}
      onTransitionEnd={(event) => {
        if (leaving && event.target === event.currentTarget && event.propertyName === "opacity") onRemove(item.id);
      }}
      className={`toast-message rounded-lg border px-3 py-2 text-sm shadow-sm ${
        item.tone === "error" ? "border-rose-200 bg-rose-50 text-danger" : "border-emerald-200 bg-emerald-50 text-emerald-800"
      }`}
    >
      {item.message}
    </div>
  );
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const push = useCallback((message: string, tone: "ok" | "error" = "error") => {
    const id = Date.now() + Math.random();
    setItems((prev) => [...prev.slice(-4), { id, message, tone }]);
  }, []);
  const remove = useCallback((id: number) => setItems((prev) => prev.filter((item) => item.id !== id)), []);
  const value = useMemo(() => ({ push }), [push]);
  return (
    <Ctx.Provider value={value}>
      {children}
      <div className="pointer-events-none fixed bottom-14 right-4 z-[60] flex w-80 max-w-[calc(100%-2rem)] flex-col gap-2">
        {items.map((item) => <ToastMessage key={item.id} item={item} onRemove={remove} />)}
      </div>
    </Ctx.Provider>
  );
}
