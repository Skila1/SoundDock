import { create } from "zustand";
import { persist } from "zustand/middleware";

type Theme = "dark" | "light" | "system";

function apply(theme: Theme) {
  const dark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.classList.toggle("light", !dark);
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", dark ? "#0c1117" : "#f4f1ea");
}

export const useTheme = create<{ theme: Theme; setTheme: (t: Theme) => void }>()(
  persist(
    (set) => ({
      theme: "dark",
      setTheme: (theme) => {
        apply(theme);
        set({ theme });
      }
    }),
    { name: "sd-theme", onRehydrateStorage: () => (s) => s && apply(s.theme) }
  )
);
