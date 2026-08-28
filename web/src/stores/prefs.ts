import { useEffect } from "react";
import { create } from "zustand";
import { persist } from "zustand/middleware";

export const DEFAULT_ACCENT = "#1db954";
export const COMPACT_CLASS = "sd-compact";

export const ACCENT_PRESETS = [
  { name: "Green", value: DEFAULT_ACCENT },
  { name: "Teal", value: "#14b8a6" },
  { name: "Sky", value: "#0ea5e9" },
  { name: "Violet", value: "#8b5cf6" },
  { name: "Rose", value: "#f43f5e" },
  { name: "Amber", value: "#f59e0b" }
] as const;

export type Density = "comfortable" | "compact";

const HEX = /^#([0-9a-f]{6})$/i;
const STYLE_ID = "sd-prefs-density";

const DENSITY_CSS = `
html.sd-compact { font-size: 15px; }
html.sd-compact header.sticky { height: 2.75rem; }
html.sd-compact main { padding-top: 0.85rem; padding-bottom: 0.85rem; }
html.sd-compact .sd-topbar { height: 2.75rem; }
`;

export function normalizeAccent(value: string) {
  return HEX.test(value) ? `#${value.slice(1).toLowerCase()}` : DEFAULT_ACCENT;
}

function ensureDensityStyles() {
  if (typeof document === "undefined") return;
  if (document.getElementById(STYLE_ID)) return;
  const el = document.createElement("style");
  el.id = STYLE_ID;
  el.textContent = DENSITY_CSS;
  document.head.appendChild(el);
}

export function syncPrefsToDom(state?: { accent?: string; density?: Density }) {
  if (typeof document === "undefined") return;
  const accent = normalizeAccent(state?.accent ?? usePrefs.getState().accent);
  const density = state?.density ?? usePrefs.getState().density;
  const root = document.documentElement;
  const dark = root.classList.contains("dark");
  root.style.setProperty("--sd-accent", accent);
  root.style.setProperty("--sd-accent-hover", `color-mix(in srgb, ${accent} 82%, ${dark ? "white" : "black"})`);
  root.style.setProperty("--sd-ring", accent);
  ensureDensityStyles();
  root.classList.toggle(COMPACT_CLASS, density === "compact");
}

type PrefsState = {
  accent: string;
  density: Density;
  keyboardShortcuts: boolean;
  setAccent: (accent: string) => void;
  setDensity: (density: Density) => void;
  toggleDensity: () => void;
  setKeyboardShortcuts: (on: boolean) => void;
};

export const usePrefs = create<PrefsState>()(
  persist(
    (set, get) => ({
      accent: DEFAULT_ACCENT,
      density: "comfortable",
      keyboardShortcuts: false,
      setAccent: (accent) => {
        const next = normalizeAccent(accent);
        syncPrefsToDom({ accent: next, density: get().density });
        set({ accent: next });
      },
      setDensity: (density) => {
        syncPrefsToDom({ accent: get().accent, density });
        set({ density });
      },
      toggleDensity: () => {
        const density: Density = get().density === "compact" ? "comfortable" : "compact";
        get().setDensity(density);
      },
      setKeyboardShortcuts: (on) => set({ keyboardShortcuts: on })
    }),
    {
      name: "sd-prefs",
      partialize: (s) => ({ accent: s.accent, density: s.density, keyboardShortcuts: s.keyboardShortcuts }),
      onRehydrateStorage: () => (s) => s && syncPrefsToDom(s)
    }
  )
);

/** Integrator: mount once in Providers/AppShell so login/setup also get accent + density. TopBar already syncs while authenticated. */
export function PrefsSync() {
  const accent = usePrefs((s) => s.accent);
  const density = usePrefs((s) => s.density);
  useEffect(() => {
    syncPrefsToDom({ accent, density });
  }, [accent, density]);
  return null;
}
