import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import type { SearchHit } from "@/types/api";

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

  if (!active) return null;

  const pick = async (it: UnmatchedItem, trackId: string) => {
    // Drop it from the list at once; the server catches up in the background.
    qc.setQueryData<UnmatchedItem[]>(key, (old) => (old || []).filter((x) => x.id !== it.id));
    setActiveId(null);
    try {
      await api.post(`/api/v1/playlists/${playlistId}/items/${it.id}/match`, { track_id: trackId });
      toast.success(`Matched ${it.title}`);
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
      <MatchSearch key={active.id} item={active} onPick={(id) => pick(active, id)} />
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

function MatchSearch({ item, onPick }: { item: UnmatchedItem; onPick: (id: string) => void }) {
  const [text, setText] = useState(() => [firstArtist(item.artists), item.title].filter(Boolean).join(" "));
  const [debounced, setDebounced] = useState(text);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(text.trim()), 250);
    return () => clearTimeout(t);
  }, [text]);
  const hits = useQuery({
    queryKey: ["unmatched-search", debounced],
    enabled: debounced.length > 1,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(debounced)}&type=track`)
  });
  const results = (hits.data?.results || []).slice(0, 8);
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
          aria-label="Search your library"
        />
      </div>
      <ul className="mt-1 max-h-72 overflow-auto">
        {results.map((h) => (
          <li key={h.id}>
            <button
              type="button"
              className="flex w-full items-center justify-between gap-3 rounded-md px-3 py-2 text-left text-sm hover:bg-surface-2"
              onClick={() => onPick(h.id)}
            >
              <span className="min-w-0 truncate">
                <span className="font-medium">{h.title}</span>
                {h.artist ? <span className="text-muted"> · {h.artist}</span> : null}
              </span>
              <span className="shrink-0 text-xs text-accent">Use this</span>
            </button>
          </li>
        ))}
        {debounced.length > 1 && !hits.isLoading && !results.length && (
          <li className="px-3 py-3 text-sm text-muted">No songs in your library match. Add it from YouTube with the main search, then pick it here.</li>
        )}
      </ul>
    </div>
  );
}
