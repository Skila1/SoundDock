import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Disc3, Loader2, Mic2, Music, Plus } from "lucide-react";
import { api } from "@/lib/api";
import type { SearchHit } from "@/types/api";
import { Input } from "@/components/ui/input";
import { useDebounced } from "@/components/media/LibraryToolbar";
import { cn } from "@/lib/utils";

export type PickKind = "track" | "album" | "artist";

const kinds: { id: PickKind; label: string }[] = [
  { id: "track", label: "Songs" },
  { id: "album", label: "Albums" },
  { id: "artist", label: "Artists" }
];

export const kindIcon = { track: Music, album: Disc3, artist: Mic2 } as const;

/** Search the library and pick a song, album or artist to protect. */
export function ExclusionPicker({ onPick, excluded }: { onPick: (kind: PickKind, hit: SearchHit) => void; excluded: Set<string> }) {
  const [kind, setKind] = useState<PickKind>("track");
  const [term, setTerm] = useState("");
  const q = useDebounced(term.trim(), 250);
  const search = useQuery({
    queryKey: ["retention-picker", kind, q],
    enabled: q.length > 1,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&type=${kind}&limit=8`)
  });
  const hits = (search.data?.results || []).filter((h) => h.type === kind);
  const Icon = kindIcon[kind];

  return (
    <div>
      <div className="flex flex-col gap-2 sm:flex-row">
        <div className="flex shrink-0 rounded-full bg-surface-2 p-1" role="tablist" aria-label="What to protect">
          {kinds.map((k) => (
            <button
              key={k.id}
              type="button"
              role="tab"
              aria-selected={kind === k.id}
              onClick={() => setKind(k.id)}
              className={cn("rounded-full px-3 py-1 text-xs font-semibold transition", kind === k.id ? "bg-surface-1 text-foreground shadow-sm" : "text-muted hover:text-foreground")}
            >
              {k.label}
            </button>
          ))}
        </div>
        <div className="relative min-w-0 flex-1">
          <Input value={term} onChange={(e) => setTerm(e.target.value)} placeholder={`Search ${kinds.find((k) => k.id === kind)?.label.toLowerCase()} to protect`} />
          {search.isFetching && <Loader2 className="absolute right-3 top-3 h-4 w-4 animate-spin text-muted" />}
        </div>
      </div>
      {q.length > 1 && (
        <ul className="mt-2 divide-y divide-border overflow-hidden rounded-xl border border-border">
          {hits.map((h) => {
            const done = excluded.has(`${kind}:${h.id}`);
            return (
              <li key={h.id}>
                <button
                  type="button"
                  disabled={done}
                  className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm transition hover:bg-surface-2 disabled:opacity-60"
                  onClick={() => {
                    onPick(kind, h);
                    setTerm("");
                  }}
                >
                  <Icon className="h-4 w-4 shrink-0 text-muted" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{h.title}</span>
                    {h.artist && kind !== "artist" && <span className="block truncate text-xs text-subtle">{h.artist}</span>}
                  </span>
                  {done ? <span className="text-xs text-subtle">Protected</span> : <Plus className="h-4 w-4 shrink-0 text-accent" />}
                </button>
              </li>
            );
          })}
          {!search.isFetching && !hits.length && <li className="px-3 py-3 text-sm text-subtle">No matches.</li>}
        </ul>
      )}
    </div>
  );
}
