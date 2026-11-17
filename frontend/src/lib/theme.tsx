import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type ThemeChoice = "light" | "dark" | "system";
const STORAGE_KEY = "fixgw-theme";

function readChoice(): ThemeChoice {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === "light" || saved === "dark" || saved === "system") return saved;
  } catch {
    /* Storage can be unavailable in private windows. */
  }
  return "system";
}

function systemDark() {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function apply(choice: ThemeChoice) {
  const dark = choice === "dark" || (choice === "system" && systemDark());
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
  return dark;
}

type ThemeCtx = { theme: ThemeChoice; resolved: "light" | "dark"; setTheme: (theme: ThemeChoice) => void };
const Ctx = createContext<ThemeCtx>({ theme: "system", resolved: "light", setTheme: () => {} });

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setChoice] = useState<ThemeChoice>(readChoice);
  const [resolved, setResolved] = useState<"light" | "dark">(() => (apply(readChoice()) ? "dark" : "light"));

  useEffect(() => {
    setResolved(apply(theme) ? "dark" : "light");
    if (theme !== "system") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const change = () => setResolved(apply("system") ? "dark" : "light");
    media.addEventListener("change", change);
    return () => media.removeEventListener("change", change);
  }, [theme]);

  const setTheme = useCallback((next: ThemeChoice) => {
    setChoice(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      /* The choice still applies to this tab. */
    }
  }, []);

  const value = useMemo(() => ({ theme, resolved, setTheme }), [theme, resolved, setTheme]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTheme() {
  return useContext(Ctx);
}
