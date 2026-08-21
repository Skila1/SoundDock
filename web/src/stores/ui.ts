import { persist } from "zustand/middleware";
import { create } from "zustand";

type UiState = {
  sidebarOpen: boolean;
  queueOpen: boolean;
  nowPlayingOpen: boolean;
  commandOpen: boolean;
  mobileNav: boolean;
  navCollapsed: boolean;
  queueCollapsed: boolean;
  libraryLayout: "grid" | "list";
  set: (p: Partial<Omit<UiState, "set">>) => void;
};

export const useUi = create<UiState>()(
  persist(
    (set) => ({
      sidebarOpen: true,
      queueOpen: false,
      nowPlayingOpen: false,
      commandOpen: false,
      mobileNav: false,
      navCollapsed: false,
      queueCollapsed: false,
      libraryLayout: "grid",
      set: (p) => set(p)
    }),
    {
      name: "sd-ui",
      partialize: (s) => ({
        libraryLayout: s.libraryLayout,
        navCollapsed: s.navCollapsed,
        queueCollapsed: s.queueCollapsed
      })
    }
  )
);
