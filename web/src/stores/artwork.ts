import { create } from "zustand";

/**
 * Artwork URLs do not change when a cover is replaced, so the browser keeps
 * showing the old image. Bumping this version (after an upload, removal, or a
 * server "artwork" invalidation) adds a cache-busting param to every cover.
 */
export const useArtworkVersion = create<{ version: number; bump: () => void }>((set) => ({
  version: 0,
  bump: () => set((s) => ({ version: s.version + 1 }))
}));

export function withArtworkVersion(src: string | undefined, version: number): string | undefined {
  if (!src || version === 0 || !src.startsWith("/api/")) return src;
  return `${src}${src.includes("?") ? "&" : "?"}v=${version}`;
}
