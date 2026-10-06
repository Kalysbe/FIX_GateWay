import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

type ToastItem = { id: number; message: string; tone: "ok" | "error" };

type ToastCtx = {
  push: (message: string, tone?: "ok" | "error") => void;
};

const Ctx = createContext<ToastCtx>({ push: () => {} });

export function useToast() {
  return useContext(Ctx);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const push = useCallback((message: string, tone: "ok" | "error" = "error") => {
    const id = Date.now() + Math.random();
    setItems((prev) => [...prev.slice(-4), { id, message, tone }]);
    window.setTimeout(() => setItems((prev) => prev.filter((t) => t.id !== id)), 4500);
  }, []);
  const value = useMemo(() => ({ push }), [push]);
  return (
    <Ctx.Provider value={value}>
      {children}
      <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-80 max-w-[calc(100%-2rem)] flex-col gap-2">
        {items.map((t) => (
          <div
            key={t.id}
            role="status"
            className={`rounded-lg border px-3 py-2 text-sm shadow-sm ${
              t.tone === "error" ? "border-rose-200 bg-rose-50 text-danger" : "border-emerald-200 bg-emerald-50 text-emerald-800"
            }`}
          >
            {t.message}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}
