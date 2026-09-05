import { persist } from "zustand/middleware";
import { create } from "zustand";

export function desktopQueue() {
  return typeof window !== "undefined" && window.matchMedia("(min-width: 1280px)").matches;
}

export function queueDocked(s: { queuePinned: boolean; queueCollapsed: boolean }) {
  return s.queuePinned || !s.queueCollapsed;
}

type UiActions = "set" | "toggleQueue" | "openQueue" | "closeQueue" | "toggleLyrics" | "openLyrics" | "closeLyrics";

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
  set: (p: Partial<Omit<UiState, UiActions>>) => void;
  toggleQueue: () => void;
  openQueue: () => void;
  closeQueue: () => void;
  toggleLyrics: () => void;
  openLyrics: () => void;
  closeLyrics: () => void;
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
        if (get().lyricsOpen) {
          get().openQueue();
          return;
        }
        if (desktopQueue()) {
          const open = queueDocked(get());
          if (open) set({ queuePinned: false, queueCollapsed: true, queueOpen: false });
          else set({ queueCollapsed: false, queueOpen: false });
          return;
        }
        set({ queueOpen: !get().queueOpen });
      },
      openQueue: () => {
        if (desktopQueue()) set({ lyricsOpen: false, queueCollapsed: false, queueOpen: false });
        else set({ lyricsOpen: false, queueOpen: true });
      },
      closeQueue: () => set({ queueCollapsed: true, queuePinned: false, queueOpen: false }),
      toggleLyrics: () => {
        if (get().lyricsOpen) get().closeLyrics();
        else get().openLyrics();
      },
      openLyrics: () => set({ lyricsOpen: true, nowPlayingOpen: false }),
      closeLyrics: () => set({ lyricsOpen: false })
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
