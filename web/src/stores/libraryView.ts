import { useCallback, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { create } from "zustand";
import { persist } from "zustand/middleware";
import { api } from "@/lib/api";
import { useUi } from "@/stores/ui";
import type { User } from "@/types/api";

export type LibraryLayout = "grid" | "list";

export type LibraryView = {
  layout?: LibraryLayout;
  sort?: string;
  desc?: boolean;
  genre?: string;
  explicit?: "all" | "clean" | "explicit";
  /** Open (true) or collapsed (false) state of named page sections. */
  sections?: Record<string, boolean>;
};

type State = {
  views: Record<string, LibraryView>;
  /** Users whose server copy has been merged in this session. */
  synced: Record<string, boolean>;
  patch: (key: string, p: Partial<LibraryView>) => void;
};

/**
 * Library view preferences (layout, sort, filters, collapsed sections), kept
 * per signed-in user and per page. The browser copy makes pages render
 * instantly; the server copy makes them follow the account across devices.
 */
const useLibraryViews = create<State>()(
  persist(
    (set) => ({
      views: {},
      synced: {},
      patch: (key, p) => set((s) => ({ views: { ...s.views, [key]: { ...s.views[key], ...p } } }))
    }),
    { name: "sd-library-views", partialize: (s) => ({ views: s.views }) }
  )
);

const pushTimers: Record<string, number> = {};

function scopedViews(userId: string) {
  const prefix = `${userId}:`;
  const out: Record<string, LibraryView> = {};
  for (const [k, v] of Object.entries(useLibraryViews.getState().views)) {
    if (k.startsWith(prefix)) out[k.slice(prefix.length)] = v;
  }
  return out;
}

function pushToServer(userId: string) {
  window.clearTimeout(pushTimers[userId]);
  pushTimers[userId] = window.setTimeout(() => {
    void api.put("/api/v1/me/ui-prefs", { views: scopedViews(userId) }).catch(() => undefined);
  }, 800);
}

async function pullFromServer(userId: string) {
  if (useLibraryViews.getState().synced[userId]) return;
  useLibraryViews.setState((s) => ({ synced: { ...s.synced, [userId]: true } }));
  try {
    const remote = await api.get<{ views?: Record<string, LibraryView> }>("/api/v1/me/ui-prefs");
    const views = remote?.views;
    if (!views || typeof views !== "object") {
      // First time on the server: upload what this browser already has.
      if (Object.keys(scopedViews(userId)).length) pushToServer(userId);
      return;
    }
    useLibraryViews.setState((s) => {
      const next = { ...s.views };
      for (const [scope, v] of Object.entries(views)) next[`${userId}:${scope}`] = { ...next[`${userId}:${scope}`], ...v };
      return { views: next };
    });
  } catch {
    /* offline: keep the local copy */
  }
}

export function useLibraryView(scope: string, defaults: LibraryView = {}) {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const userId = me.data?.id || "";
  const key = `${userId || "anon"}:${scope}`;
  const stored = useLibraryViews((s) => s.views[key]);
  const fallbackLayout = useUi((s) => s.libraryLayout);
  const patchStore = useLibraryViews((s) => s.patch);
  useEffect(() => {
    if (userId) void pullFromServer(userId);
  }, [userId]);
  const view: Required<Pick<LibraryView, "layout" | "explicit">> & LibraryView = {
    explicit: "all",
    ...defaults,
    ...stored,
    layout: stored?.layout || defaults.layout || fallbackLayout
  };
  const patch = useCallback(
    (p: Partial<LibraryView>) => {
      patchStore(key, p);
      if (userId) pushToServer(userId);
    },
    [patchStore, key, userId]
  );
  return [view, patch] as const;
}
