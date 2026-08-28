import type { QueryClient } from "@tanstack/react-query";

const CATALOGUE_KEYS = [
  ["home"],
  ["tracks"],
  ["albums"],
  ["album"],
  ["artists"],
  ["artist"],
  ["favourites"],
  ["search"],
  ["me-history"],
  ["history"],
  ["playlists"],
  ["playlist"],
  ["track-meta"]
] as const;

export function catalogueRowId(row: unknown): string | undefined {
  if (!row || typeof row !== "object") return undefined;
  const r = row as { id?: unknown; track_id?: unknown };
  if (typeof r.id === "string" && r.id) return r.id;
  if (typeof r.track_id === "string" && r.track_id) return r.track_id;
  return undefined;
}

function filterRows<T>(rows: T[] | undefined, gone: Set<string>): T[] | undefined {
  if (!Array.isArray(rows)) return rows;
  return rows.filter((row) => {
    const id = catalogueRowId(row);
    return !id || !gone.has(id);
  });
}

function mapTrackCache(old: unknown, mapRows: (rows: unknown[]) => unknown[]): unknown {
  if (Array.isArray(old)) return mapRows(old);
  if (!old || typeof old !== "object") return old;
  const data = old as { items?: unknown[]; pages?: { items?: unknown[] }[] };
  if (Array.isArray(data.items)) return { ...data, items: mapRows(data.items) };
  if (Array.isArray(data.pages)) {
    return {
      ...data,
      pages: data.pages.map((p) => (p && Array.isArray(p.items) ? { ...p, items: mapRows(p.items) } : p))
    };
  }
  return old;
}

function dropFromTrackList(old: unknown, gone: Set<string>) {
  if (!old || typeof old !== "object") return old;
  const data = old as { tracks?: unknown[] };
  if (!Array.isArray(data.tracks)) return old;
  return { ...data, tracks: filterRows(data.tracks, gone) };
}

export function removeTracksFromCaches(qc: QueryClient, ids: string[]) {
  const gone = new Set(ids);
  qc.setQueriesData({ queryKey: ["home"] }, (old: unknown) => {
    if (!old || typeof old !== "object") return old;
    const data = old as Record<string, unknown>;
    return {
      ...data,
      continue: filterRows(data.continue as unknown[], gone),
      recently_added: filterRows(data.recently_added as unknown[], gone),
      most_played: filterRows(data.most_played as unknown[], gone)
    };
  });
  qc.setQueriesData({ queryKey: ["tracks"] }, (old: unknown) => mapTrackCache(old, (rows) => filterRows(rows, gone) || []));
  qc.setQueriesData({ queryKey: ["search"] }, (old: unknown) => {
    if (!old || typeof old !== "object") return old;
    const data = old as { results?: unknown[] };
    if (!Array.isArray(data.results)) return old;
    return {
      ...data,
      results: data.results.filter((hit) => {
        const h = hit as { type?: string; id?: string };
        return h.type !== "track" || !h.id || !gone.has(h.id);
      })
    };
  });
  qc.setQueriesData({ queryKey: ["favourites"] }, (old: unknown) => {
    if (!Array.isArray(old)) return old;
    return old.filter((row) => {
      const f = row as { type?: string; id?: string };
      return f.type !== "track" || !f.id || !gone.has(f.id);
    });
  });
  qc.setQueriesData({ queryKey: ["me-history"] }, (old: unknown) => (Array.isArray(old) ? filterRows(old, gone) : old));
  qc.setQueriesData({ queryKey: ["album"] }, (old: unknown) => dropFromTrackList(old, gone));
  qc.setQueriesData({ queryKey: ["playlist"] }, (old: unknown) => dropFromTrackList(old, gone));
}

export function clearCatalogueTracks(qc: QueryClient) {
  qc.setQueryData(["home"], (old: unknown) => {
    const empty = { continue: [], recently_added: [], most_played: [] };
    if (!old || typeof old !== "object") return empty;
    return { ...(old as object), ...empty };
  });
  qc.setQueryData(["tracks"], []);
}

export function patchTracksInCaches(qc: QueryClient, ids: string[], patch: { genre?: string; year?: number }) {
  const want = new Set(ids);
  const apply = (row: unknown) => {
    const id = catalogueRowId(row);
    if (!id || !want.has(id) || !row || typeof row !== "object") return row;
    return { ...(row as object), ...patch };
  };
  qc.setQueriesData({ queryKey: ["tracks"] }, (old: unknown) => mapTrackCache(old, (rows) => rows.map(apply)));
  qc.setQueriesData({ queryKey: ["home"] }, (old: unknown) => {
    if (!old || typeof old !== "object") return old;
    const data = old as Record<string, unknown>;
    const mapRows = (rows: unknown) => (Array.isArray(rows) ? rows.map(apply) : rows);
    return {
      ...data,
      continue: mapRows(data.continue),
      recently_added: mapRows(data.recently_added),
      most_played: mapRows(data.most_played)
    };
  });
}

export function refreshCatalogue(qc: QueryClient) {
  for (const queryKey of CATALOGUE_KEYS) {
    qc.invalidateQueries({ queryKey: [...queryKey] });
  }
}
