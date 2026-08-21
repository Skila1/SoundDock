import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { toast } from "sonner";
import type { SearchHit } from "@/types/api";

export function UnmatchedPanel({ playlistId }: { playlistId: string }) {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["unmatched", playlistId],
    queryFn: () => api.get<any[]>(`/api/v1/playlists/${playlistId}/unmatched`)
  });
  const items = q.data || [];
  if (!items.length) return null;
  return (
    <section className="mb-8 rounded-xl border border-border bg-surface-1 p-4">
      <h2 className="mb-1 font-semibold">Needs review</h2>
      <p className="mb-4 text-sm text-muted">{items.length} items are not in your SoundDock library. Playback skips them.</p>
      <ul className="space-y-3">
        {items.map((it) => (
          <li key={it.id} className="rounded-lg bg-surface-2 p-3">
            <div className="flex items-start justify-between gap-2">
              <div>
                <div className="font-medium">{it.title}</div>
                <div className="text-xs text-muted">{it.artists}{it.album ? ` · ${it.album}` : ""}{it.isrc ? ` · ${it.isrc}` : ""}</div>
                <Badge className="mt-1" tone={it.match_status === "ambiguous" ? "warning" : "neutral"}>{it.match_status}</Badge>
              </div>
            </div>
            <MatchSearch
              onPick={async (trackId) => {
                await api.post(`/api/v1/playlists/${playlistId}/items/${it.id}/match`, { track_id: trackId });
                toast.success("Matched");
                qc.invalidateQueries({ queryKey: ["playlist", playlistId] });
                qc.invalidateQueries({ queryKey: ["unmatched", playlistId] });
              }}
            />
          </li>
        ))}
      </ul>
    </section>
  );
}

function MatchSearch({ onPick }: { onPick: (id: string) => void }) {
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  return (
    <div className="mt-2">
      <form
        className="flex gap-2"
        onSubmit={async (e) => {
          e.preventDefault();
          const r = await api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&type=track`);
          setHits(r.results || []);
        }}
      >
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search your library to match…" />
        <Button type="submit" size="sm" variant="secondary">Search</Button>
      </form>
      <ul className="mt-2 space-y-1">
        {hits.slice(0, 5).map((h) => (
          <li key={h.id}>
            <button className="text-sm text-accent hover:underline" onClick={() => onPick(h.id)}>
              {h.title}{h.artist ? ` - ${h.artist}` : ""}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
