import { useCallback } from "react";
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
};

type State = {
  views: Record<string, LibraryView>;
  patch: (key: string, p: Partial<LibraryView>) => void;
};

/**
 * Library view preferences (layout, sort, filters) kept per signed-in user and
 * per page, so two people sharing a browser each get their own setup.
 */
const useLibraryViews = create<State>()(
  persist(
    (set) => ({
      views: {},
      patch: (key, p) => set((s) => ({ views: { ...s.views, [key]: { ...s.views[key], ...p } } }))
    }),
    { name: "sd-library-views" }
  )
);

export function useLibraryView(scope: string, defaults: LibraryView = {}) {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const key = `${me.data?.id || "anon"}:${scope}`;
  const stored = useLibraryViews((s) => s.views[key]);
  const fallbackLayout = useUi((s) => s.libraryLayout);
  const patchStore = useLibraryViews((s) => s.patch);
  const view: Required<Pick<LibraryView, "layout" | "explicit">> & LibraryView = {
    explicit: "all",
    ...defaults,
    ...stored,
    layout: stored?.layout || defaults.layout || fallbackLayout
  };
  const patch = useCallback((p: Partial<LibraryView>) => patchStore(key, p), [patchStore, key]);
  return [view, patch] as const;
}
