import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";
import { toast } from "sonner";
import { Toaster } from "./ui/sonner";

type ToastCtx = {
  push: (message: string, tone?: "ok" | "error") => void;
};

const Ctx = createContext<ToastCtx>({ push: () => {} });

export function useToast() {
  return useContext(Ctx);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const push = useCallback((message: string, tone: "ok" | "error" = "error") => {
    if (tone === "ok") toast.success(message);
    else toast.error(message);
  }, []);
  const value = useMemo(() => ({ push }), [push]);
  return (
    <Ctx.Provider value={value}>
      {children}
      <Toaster />
    </Ctx.Provider>
  );
}
