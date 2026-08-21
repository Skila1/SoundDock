import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Search } from "lucide-react";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { MediaCard } from "@/components/media/MediaCard";
import { TrackList } from "@/components/media/TrackList";
import { EmptyState } from "@/components/ui/empty";
import { usePlayer } from "@/stores/player";
import type { SearchHit } from "@/types/api";
import { toast } from "sonner";

export function SearchPage() {
  const [sp, setSp] = useSearchParams();
  const [q, setQ] = useState(sp.get("q") || "");
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const results = useQuery({
    queryKey: ["search", q],
    enabled: q.trim().length > 0,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&limit=40`)
  });
  const grouped = useMemo(() => {
    const hits = results.data?.results || [];
    return {
      track: hits.filter((h) => h.type === "track"),
      album: hits.filter((h) => h.type === "album"),
      artist: hits.filter((h) => h.type === "artist"),
      playlist: hits.filter((h) => h.type === "playlist")
    };
  }, [results.data]);

  return (
    <div>
      <h1 className="mb-4 text-3xl font-semibold">Search</h1>
      <Input
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setSp({ q: e.target.value });
        }}
        placeholder="Tracks, albums, artists, playlists"
        className="mb-6 max-w-xl"
      />
      {!q && <p className="text-muted">Search your SoundDock library.</p>}
      {q && !results.data?.results?.length && !results.isLoading && (
        <EmptyState icon={Search} title={`No results for ‘${q}’.`} description="Try another spelling or a broader query." />
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
            tracks={grouped.track.map((h) => ({ id: h.id, title: h.title, artist: h.artist, album: h.album, duration_ms: h.duration_ms }))}
            onPlay={(i) => play(grouped.track.map((h) => h.id), i)}
            onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
            onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
          />
        </section>
      )}
    </div>
  );
}
