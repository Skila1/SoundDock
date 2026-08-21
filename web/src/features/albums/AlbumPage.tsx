import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { Heart, ListPlus, Play, Shuffle } from "lucide-react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { TrackList } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { artworkUrl, formatDuration } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Album, Track } from "@/types/api";
import { toast } from "sonner";

export function AlbumPage() {
  const { id } = useParams();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const q = useQuery({ queryKey: ["album", id], queryFn: () => api.get<Album>(`/api/v1/albums/${id}`) });
  const a = q.data;
  if (!a) return <div className="h-64 animate-pulse rounded-xl bg-surface-2" />;
  const tracks = a.tracks || [];
  const ids = tracks.map((t) => t.id);
  const total = tracks.reduce((s, t) => s + (t.duration_ms || 0), 0);
  const discs = new Map<number, Track[]>();
  tracks.forEach((t) => {
    const d = t.disc_number || 1;
    discs.set(d, [...(discs.get(d) || []), t]);
  });
  return (
    <div>
      <div className="mb-8 flex flex-col gap-6 md:flex-row">
        <div className="h-52 w-52 overflow-hidden rounded-xl shadow-card">
          <Artwork src={artworkUrl("album", a.id, "page")} id={a.id} name={a.title} kind="album" />
        </div>
        <div className="flex flex-col justify-end">
          <p className="text-xs uppercase tracking-widest text-subtle">{a.is_compilation ? "Compilation" : "Album"}</p>
          <h1 className="text-4xl font-semibold md:text-5xl">{a.title}</h1>
          <p className="mt-2 text-muted">{a.artist}{a.edition_title ? ` · ${a.edition_title}` : ""}{a.year ? ` · ${a.year}` : ""}</p>
          <p className="text-sm text-subtle">{tracks.length} tracks · {formatDuration(total)}</p>
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={() => play(ids)}><Play className="fill-current" /> Play</Button>
            <Button variant="secondary" onClick={() => play([...ids].sort(() => Math.random() - 0.5))}><Shuffle /> Shuffle</Button>
            <Button variant="ghost" onClick={() => api.post("/api/v1/favourites", { type: "album", id: a.id, on: true }).then(() => toast.success("Favourited"))}><Heart /></Button>
            <Button variant="ghost" onClick={() => add(ids).then(() => toast.success("Added to queue"))}><ListPlus /> Add to queue</Button>
          </div>
        </div>
      </div>
      {[...discs.entries()].map(([disc, list]) => (
        <section key={disc} className="mb-6">
          {discs.size > 1 && <h2 className="mb-2 text-sm font-semibold text-muted">Disc {disc}</h2>}
          <TrackList
            tracks={list}
            showAlbum={false}
            onPlay={(i) => play(ids, ids.indexOf(list[i].id))}
            onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
            onNext={(t) => add([t.id], true)}
            onFav={(t) => api.post("/api/v1/favourites", { type: "track", id: t.id, on: true }).then(() => toast.success("Favourited"))}
          />
        </section>
      ))}
    </div>
  );
}
