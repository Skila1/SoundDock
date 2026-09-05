import { persist } from "zustand/middleware";
import { create } from "zustand";

function desktopQueue() {
  return typeof window !== "undefined" && window.matchMedia("(min-width: 1280px)").matches;
}

type UiState = {
  sidebarOpen: boolean;
  queueOpen: boolean;
  nowPlayingOpen: boolean;
  lyricsOpen: boolean;
  commandOpen: boolean;
  mobileNav: boolean;
  navCollapsed: boolean;
  queueCollapsed: boolean;
  queuePinned: boolean;
  libraryLayout: "grid" | "list";
  set: (p: Partial<Omit<UiState, "set" | "toggleQueue" | "openQueue" | "closeQueue">>) => void;
  toggleQueue: () => void;
  openQueue: () => void;
  closeQueue: () => void;
};

export const useUi = create<UiState>()(
  persist(
    (set, get) => ({
      sidebarOpen: true,
      queueOpen: false,
      nowPlayingOpen: false,
      lyricsOpen: false,
      commandOpen: false,
      mobileNav: false,
      navCollapsed: false,
      queueCollapsed: true,
      queuePinned: false,
      libraryLayout: "grid",
      set: (p) => set(p),
      toggleQueue: () => {
        if (desktopQueue()) {
          const open = get().queuePinned || !get().queueCollapsed;
          if (open) set({ queuePinned: false, queueCollapsed: true, queueOpen: false });
          else set({ queueCollapsed: false, queueOpen: false });
          return;
        }
        set({ queueOpen: !get().queueOpen });
      },
      openQueue: () => {
        if (desktopQueue()) set({ queueCollapsed: false, queueOpen: false });
        else set({ queueOpen: true });
      },
      closeQueue: () => set({ queueCollapsed: true, queuePinned: false, queueOpen: false })
    }),
    {
      name: "sd-ui",
      partialize: (s) => ({
        libraryLayout: s.libraryLayout,
        navCollapsed: s.navCollapsed,
        queueCollapsed: s.queueCollapsed,
        queuePinned: s.queuePinned
      })
    }
  )
);
