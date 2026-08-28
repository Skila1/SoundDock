import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Search } from "lucide-react";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { MediaCard } from "@/components/media/MediaCard";
import { TrackList } from "@/components/media/TrackList";
import { EmptyState } from "@/components/ui/empty";
import { usePlayer, type PlayerQueueItem } from "@/stores/player";
import type { SearchHit } from "@/types/api";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

const FILTERS: { id: string; label: string; token: string }[] = [
  { id: "never", label: "Never played", token: "played:never" },
  { id: "recent", label: "Last 7 days", token: "last_played:7d" },
  { id: "month", label: "Last 30 days", token: "last_played:30d" },
  { id: "stale", label: "Not in 90 days", token: "last_played:>90d" }
];

function hitKeys(hit: SearchHit): string[] {
  const extra = hit as SearchHit & { youtube_id?: string; external_id?: string };
  return [hit.id, extra.youtube_id, extra.external_id].filter((v): v is string => !!v);
}

function itemKeys(item: PlayerQueueItem): string[] {
  const keys = [item.track_id, item.origin, item.youtube_id, item.external_id, item.id].filter((v): v is string => !!v);
  const expanded: string[] = [];
  for (const k of keys) {
    expanded.push(k);
    if (k.startsWith("youtube:")) expanded.push(k.slice("youtube:".length));
  }
  return expanded;
}

function queueMediaStateForHit(items: PlayerQueueItem[] | undefined, hit: SearchHit): string | undefined {
  if (!items?.length) return undefined;
  const want = new Set(hitKeys(hit));
  for (const item of items) {
    if (itemKeys(item).some((k) => want.has(k))) return item.media_state;
  }
  return undefined;
}

function mediaStateChip(state?: string): string | null {
  if (state === "restoring") return "Restoring";
  if (state === "missing_external" || state === "failed") return "Unavailable";
  if (state === "ready") return "Ready";
  return null;
}

function stripPlayTokens(q: string) {
  return q
    .replace(/\b(played|never_played|neverplayed|last_played|lastplayed):("[^"]*"|'[^']*'|\S+)/gi, "")
    .replace(/\s+/g, " ")
    .trim();
}

function activeFilter(q: string) {
  return FILTERS.find((f) => q.toLowerCase().includes(f.token.toLowerCase()))?.id || "";
}

export function SearchPage() {
  const [sp, setSp] = useSearchParams();
  const [q, setQ] = useState(sp.get("q") || "");
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const queueItems = usePlayer((s) => s.queue?.items);
  const results = useQuery({
    queryKey: ["search", q],
    enabled: q.trim().length > 0,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&limit=40`)
  });
  const youtubeQ = stripPlayTokens(q);
  const youtube = useQuery({
    queryKey: ["search-youtube", q],
    enabled: youtubeQ.length > 1,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search/youtube?q=${encodeURIComponent(q)}&limit=12`)
  });
  const grouped = useMemo(() => {
    const hits = results.data?.results || [];
    const tracks = hits.filter((h) => h.type === "track");
    const yt = (youtube.data?.results || []).filter((h) => {
      if (h.type !== "youtube") return false;
      const nt = h.title.trim().toLowerCase();
      const na = (h.artist || "").trim().toLowerCase();
      return !tracks.some((t) => {
        if (t.title.trim().toLowerCase() !== nt) return false;
        if (!na) return true;
        const la = (t.artist || "").trim().toLowerCase();
        return la.includes(na) || na.includes(la);
      });
    });
    return {
      track: tracks,
      youtube: yt,
      album: hits.filter((h) => h.type === "album"),
      artist: hits.filter((h) => h.type === "artist"),
      playlist: hits.filter((h) => h.type === "playlist")
    };
  }, [results.data, youtube.data]);
  const currentFilter = activeFilter(q);

  const applyFilter = (token: string) => {
    const base = stripPlayTokens(q);
    const next = currentFilter && FILTERS.find((f) => f.id === currentFilter)?.token === token ? base : [base, token].filter(Boolean).join(" ");
    setQ(next);
    setSp({ q: next });
  };

  return (
    <div>
      <h1 className="mb-4 text-3xl font-semibold">Search</h1>
      <Input
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setSp({ q: e.target.value });
        }}
        placeholder="Tracks, albums, artists, playlists, or played:never last_played:7d"
        className="mb-3 max-w-xl"
      />
      <div className="mb-6 flex flex-wrap gap-2">
        {FILTERS.map((f) => (
          <Button
            key={f.id}
            size="sm"
            variant={currentFilter === f.id ? "default" : "secondary"}
            className={cn(currentFilter === f.id && "pointer-events-auto")}
            onClick={() => applyFilter(f.token)}
          >
            {f.label}
          </Button>
        ))}
      </div>
      {!q && <p className="text-muted">Search your SoundDock library. Missing tracks can be fetched from YouTube.</p>}
      {q && !results.isLoading && !youtube.isLoading && !results.data?.results?.length && !grouped.youtube.length && (
        <EmptyState icon={Search} title={`No results for ‘${q}’.`} description="Try another spelling. YouTube search runs inside SoundDock when enabled." />
      )}
      {grouped.artist.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 font-semibold">Artists</h2>
          <div className="flex gap-4 overflow-x-auto">{grouped.artist.map((h) => <MediaCard key={h.id} to={`/artists/${h.id}`} id={h.id} title={h.title} kind="artist" />)}</div>
        </section>
      )}
      {grouped.album.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 font-semibold">Albums</h2>
          <div className="flex gap-4 overflow-x-auto">{grouped.album.map((h) => <MediaCard key={h.id} to={`/albums/${h.id}`} id={h.id} title={h.title} subtitle={h.artist} kind="album" />)}</div>
        </section>
      )}
      {grouped.track.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 font-semibold">Tracks</h2>
          <TrackList
            tracks={grouped.track.map((h) => {
              const extra = h as SearchHit & { explicit?: boolean | null };
              return {
                id: h.id,
                title: h.title,
                artist: h.artist,
                album: h.album,
                duration_ms: h.duration_ms,
                codec: h.codec,
                explicit: extra.explicit,
                year: h.year
              };
            })}
            onPlay={(i) => play([grouped.track[i].id])}
            onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
            onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
          />
        </section>
      )}
      {grouped.youtube.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 font-semibold">YouTube</h2>
          <p className="mb-3 text-sm text-muted">Not in your library. Play or queue to download into SoundDock.</p>
          <TrackList
            tracks={grouped.youtube.map((h) => {
              const chip = mediaStateChip(queueMediaStateForHit(queueItems, h));
              return {
                id: h.id,
                title: h.title,
                artist: h.artist,
                album: h.album,
                duration_ms: h.duration_ms,
                source: "youtube",
                artwork_url: h.artwork_url,
                ...(chip ? { codec: chip } : {})
              };
            })}
            onPlay={(i) => play([grouped.youtube[i].id])}
            onQueue={(t) => add([t.id]).then(() => toast.success("Downloading and adding to queue"))}
            onNext={(t) => add([t.id], true).then(() => toast.success("Downloading to play next"))}
          />
        </section>
      )}
      {grouped.playlist.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 font-semibold">Playlists</h2>
          <div className="flex gap-4 overflow-x-auto">
            {grouped.playlist.map((h) => <MediaCard key={h.id} to={`/playlists/${h.id}`} id={h.id} title={h.title} kind="playlist" />)}
          </div>
        </section>
      )}
    </div>
  );
}
