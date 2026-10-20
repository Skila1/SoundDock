import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { artworkUrl, cn, formatDuration } from "@/lib/utils";
import { toast } from "sonner";
import { hasPerm } from "@/lib/perms";
import type { SearchHit, User, YoutubeSearchResponse } from "@/types/api";

type UnmatchedItem = {
  id: string;
  title: string;
  artists?: string;
  album?: string;
  isrc?: string;
  match_status?: string;
  match_confidence?: number | null;
};

function reason(it: UnmatchedItem) {
  if (it.match_status === "ambiguous") return "Several close matches";
  if (it.match_status === "failed") return "Download failed";
  return "Not found automatically";
}

function firstArtist(s?: string) {
  return (s || "").split(",")[0].trim();
}

export function UnmatchedPanel({ playlistId }: { playlistId: string }) {
  const qc = useQueryClient();
  const key = ["unmatched", playlistId];
  const q = useQuery({
    queryKey: key,
    queryFn: () => api.get<UnmatchedItem[]>(`/api/v1/playlists/${playlistId}/unmatched`)
  });
  const items = q.data || [];
  const [activeId, setActiveId] = useState<string | null>(null);
  const active = items.find((it) => it.id === activeId) || items[0];

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const canYouTube = hasPerm(me.data, "playlists.external_import");

  if (!active) return null;

  const pick = async (it: UnmatchedItem, hit: SearchHit) => {
    // Drop it from the list at once; the server catches up in the background.
    qc.setQueryData<UnmatchedItem[]>(key, (old) => (old || []).filter((x) => x.id !== it.id));
    setActiveId(null);
    try {
      if (hit.type === "youtube") {
        await api.post(`/api/v1/playlists/${playlistId}/items/${it.id}/youtube`, { video_id: hit.id });
        toast.success(`Downloading ${it.title}`);
      } else {
        await api.post(`/api/v1/playlists/${playlistId}/items/${it.id}/match`, { track_id: hit.id });
        toast.success(`Matched ${it.title}`);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not match");
      qc.invalidateQueries({ queryKey: key });
    }
    qc.invalidateQueries({ queryKey: ["playlist", playlistId] });
    qc.invalidateQueries({ queryKey: ["sync-diff", playlistId] });
  };

  return (
    <section className="mb-8 rounded-2xl border border-border bg-surface-1 p-5 shadow-sm">
      <h2 className="mb-1 font-semibold">Needs review</h2>
      <p className="mb-4 text-sm text-muted">
        {items.length} {items.length === 1 ? "song was" : "songs were"} not found automatically. Pick one and choose the right song.
      </p>
      <MatchSearch key={active.id} item={active} youtube={canYouTube} onPick={(h) => pick(active, h)} />
      <ul className="mt-4 space-y-1">
        {items.map((it) => (
          <li key={it.id}>
            <button
              type="button"
              onClick={() => setActiveId(it.id)}
              className={cn(
                "flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-surface-2",
                it.id === active.id && "bg-accent/10 ring-1 ring-inset ring-accent/40"
              )}
            >
              <span className="min-w-0">
                <span className="block truncate font-medium">{it.title}</span>
                <span className="block truncate text-xs text-muted">{it.artists}{it.album ? ` · ${it.album}` : ""}</span>
              </span>
              <span className="shrink-0 text-xs text-subtle">{reason(it)}</span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function MatchSearch({ item, youtube, onPick }: { item: UnmatchedItem; youtube: boolean; onPick: (h: SearchHit) => void }) {
  const [text, setText] = useState(() => [firstArtist(item.artists), item.title].filter(Boolean).join(" "));
  const [debounced, setDebounced] = useState(text);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(text.trim()), 300);
    return () => clearTimeout(t);
  }, [text]);
  const ready = debounced.length > 1;
  const lib = useQuery({
    queryKey: ["unmatched-search", debounced],
    enabled: ready,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(debounced)}&type=track`)
  });
  const yt = useQuery({
    queryKey: ["unmatched-search-yt", debounced],
    enabled: ready && youtube,
    staleTime: 60_000,
    queryFn: () => api.get<YoutubeSearchResponse>(`/api/v1/search/youtube?q=${encodeURIComponent(debounced)}&limit=6`)
  });
  const libHits = (lib.data?.results || []).slice(0, 5);
  const ytHits = (yt.data?.results || []).filter((h) => h.type === "youtube").slice(0, 6);
  const row = (h: SearchHit) => (
    <li key={h.type + h.id}>
      <button
        type="button"
        className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm hover:bg-surface-2"
        onClick={() => onPick(h)}
      >
        {h.type === "youtube" && (
          <img src={h.artwork_url || artworkUrl("youtube", h.id, "thumb")} alt="" className="h-8 w-12 shrink-0 rounded object-cover" />
        )}
        <span className="min-w-0 flex-1 truncate">
          <span className="font-medium">{h.title}</span>
          {h.artist ? <span className="text-muted"> · {h.artist}</span> : null}
          {h.duration_ms ? <span className="text-subtle"> · {formatDuration(h.duration_ms)}</span> : null}
        </span>
        <span className="shrink-0 text-xs text-accent">{h.type === "youtube" ? "Download this" : "Use this"}</span>
      </button>
    </li>
  );
  return (
    <div className="rounded-xl border border-border bg-surface-2/60 p-2">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-subtle" />
        <Input
          autoFocus
          className="pl-9"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={`Search for "${item.title}"`}
          aria-label="Search your library and YouTube"
        />
      </div>
      {ready && (
        <div className="mt-1 max-h-96 overflow-auto">
          <p className="px-3 pt-2 text-[11px] font-semibold uppercase tracking-wider text-subtle">Library</p>
          <ul>{libHits.map(row)}</ul>
          {!lib.isLoading && !libHits.length && <p className="px-3 py-2 text-sm text-muted">Nothing in your library.</p>}
          {youtube && (
            <>
              <p className="px-3 pt-3 text-[11px] font-semibold uppercase tracking-wider text-subtle">YouTube</p>
              {yt.isLoading && <p className="px-3 py-2 text-sm text-muted">Searching YouTube…</p>}
              <ul>{ytHits.map(row)}</ul>
              {!yt.isLoading && !ytHits.length && <p className="px-3 py-2 text-sm text-muted">{yt.isError ? "YouTube search failed." : "No YouTube results."}</p>}
            </>
          )}
        </div>
      )}
    </div>
  );
}
