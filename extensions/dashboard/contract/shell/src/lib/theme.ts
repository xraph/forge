import { create } from "zustand";

export type Theme = "light" | "dark" | "system";

interface ThemeState {
  theme: Theme;
  /** resolved is the actual mode applied to the DOM (light or dark, never "system"). */
  resolved: "light" | "dark";
  setTheme: (t: Theme) => void;
  /** Reads from localStorage + system preference on first run; safe to call repeatedly. */
  init: () => void;
}

const STORAGE_KEY = "forge.dashboard.theme";

function readStoredTheme(): Theme {
  if (typeof window === "undefined") return "system";
  const v = window.localStorage.getItem(STORAGE_KEY);
  if (v === "light" || v === "dark" || v === "system") return v;
  return "system";
}

function systemPrefersDark(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function resolve(theme: Theme): "light" | "dark" {
  if (theme === "system") return systemPrefersDark() ? "dark" : "light";
  return theme;
}

function applyToDocument(resolved: "light" | "dark"): void {
  if (typeof document === "undefined") return;
  const html = document.documentElement;
  if (resolved === "dark") html.classList.add("dark");
  else html.classList.remove("dark");
}

export const useThemeStore = create<ThemeState>((set, get) => ({
  theme: "system",
  resolved: "light",
  setTheme(t) {
    if (typeof window !== "undefined") {
      window.localStorage.setItem(STORAGE_KEY, t);
    }
    const resolved = resolve(t);
    applyToDocument(resolved);
    set({ theme: t, resolved });
  },
  init() {
    const stored = readStoredTheme();
    const resolved = resolve(stored);
    applyToDocument(resolved);
    set({ theme: stored, resolved });

    // Listen for system preference changes when user is on "system".
    if (typeof window !== "undefined" && window.matchMedia) {
      const mql = window.matchMedia("(prefers-color-scheme: dark)");
      mql.addEventListener("change", () => {
        if (get().theme !== "system") return;
        const r = systemPrefersDark() ? "dark" : "light";
        applyToDocument(r);
        set({ resolved: r });
      });
    }
  },
}));
