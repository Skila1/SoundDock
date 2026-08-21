import type { Track } from "@/types/api";

export type ListenSource = "web" | "discord" | "import" | string;

export type ListenTrack = Track & {
  played_at?: string;
  source?: ListenSource;
  count?: number;
  plays?: number;
  skip_count?: number;
  last_played_at?: string;
  listened_ms?: number;
};

export type ListenTotals = {
  plays: number;
  unique_tracks: number;
  unique_artists?: number;
  unique_albums?: number;
  minutes: number;
  skips?: number;
};

export type ImportedTotals = {
  plays: number;
  unique_tracks: number;
  minutes: number;
  labelled: boolean;
};

export type RankedArtist = { id: string; name: string; plays: number; listened_ms?: number };
export type RankedAlbum = { id: string; title: string; artist?: string; plays: number; listened_ms?: number };
export type RankedGenre = { genre: string; plays: number };
export type TrendBucket = { bucket: string; plays: number; listened_ms?: number };

export type StatsResponse = {
  period: string;
  from?: string | null;
  to?: string;
  sources: string[];
  include_import: boolean;
  totals: ListenTotals;
  imported: ImportedTotals;
  top_tracks: ListenTrack[];
  top_artists: RankedArtist[];
  top_albums: RankedAlbum[];
  by_bucket: TrendBucket[];
};

export type WrappedResponse = {
  year: number;
  month: number;
  from: string;
  to: string;
  sources: string[];
  include_import: boolean;
  totals: ListenTotals;
  imported: ImportedTotals;
  top_tracks: ListenTrack[];
  top_artists: RankedArtist[];
  top_albums: RankedAlbum[];
  top_genres: RankedGenre[];
  most_skipped: ListenTrack[];
  first_listen?: ListenTrack | null;
  peak_day?: { day: string; plays: number; minutes: number } | null;
  by_bucket: TrendBucket[];
};

export function asListenTracks(rows: any[] | undefined): ListenTrack[] {
  return (rows || []).map((t) => ({
    id: t.id || t.track_id,
    title: t.title,
    artist: t.artist,
    album: t.album,
    album_id: t.album_id,
    duration_ms: t.duration_ms,
    played_at: t.played_at,
    source: t.source,
    count: t.count ?? t.plays,
    plays: t.plays ?? t.count,
    skip_count: t.skip_count,
    last_played_at: t.last_played_at,
    listened_ms: t.listened_ms
  }));
}

export function formatMinutes(n?: number | null) {
  const v = Math.max(0, Math.round(n || 0));
  if (v < 60) return `${v} min`;
  const h = Math.floor(v / 60);
  const m = v % 60;
  return m ? `${h}h ${m}m` : `${h}h`;
}

export function sourceLabel(source?: string) {
  if (source === "import") return "Imported";
  if (source === "discord") return "Discord";
  if (source === "web") return "Web";
  return source || "";
}
