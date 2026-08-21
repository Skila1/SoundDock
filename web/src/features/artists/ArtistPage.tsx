import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { Heart, Play, Shuffle } from "lucide-react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { MediaCard } from "@/components/media/MediaCard";
import { TrackList } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { artworkUrl } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Artist } from "@/types/api";
import { toast } from "sonner";

export function ArtistPage() {
  const { id } = useParams();
  const play = usePlayer((s) => s.playTracks);
  const q = useQuery({ queryKey: ["artist", id], queryFn: () => api.get<Artist>(`/api/v1/artists/${id}`) });
  const a = q.data;
  if (!a) return <div className="h-48 animate-pulse rounded-xl bg-surface-2" />;
  const albums = (a.albums || []).filter((x) => !x.is_compilation);
  const comps = (a.albums || []).filter((x) => x.is_compilation);
  const ids = (a.tracks || []).map((t) => t.id);
  return (
    <div>
      <div className="mb-8 flex flex-col gap-6 md:flex-row md:items-end">
        <div className="h-40 w-40 overflow-hidden rounded-full shadow-card md:h-52 md:w-52">
          <Artwork src={artworkUrl("artist", a.id, "page")} id={a.id} name={a.name} kind="artist" />
        </div>
        <div>
          <p className="text-xs uppercase tracking-widest text-subtle">Artist</p>
          <h1 className="text-4xl font-semibold md:text-6xl">{a.name}</h1>
          <p className="mt-2 text-sm text-muted">{albums.length} albums · {(a.tracks || []).length} tracks</p>
          <div className="mt-4 flex gap-2">
            <Button disabled={!ids.length} onClick={() => play(ids)}><Play className="fill-current" /> Play</Button>
            <Button variant="secondary" disabled={!ids.length} onClick={() => play([...ids].sort(() => Math.random() - 0.5))}><Shuffle /> Shuffle</Button>
            <Button variant="ghost" onClick={() => api.post("/api/v1/favourites", { type: "artist", id: a.id, on: true }).then(() => toast.success("Favourited"))}><Heart /></Button>
          </div>
        </div>
      </div>
      {(a.tracks || []).length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 text-lg font-semibold">Popular</h2>
          <TrackList tracks={a.tracks || []} onPlay={(i) => play(ids, i)} showAlbum />
        </section>
      )}
      {albums.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 text-lg font-semibold">Albums</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
            {albums.map((al) => <MediaCard key={al.id} className="max-w-none min-w-0" to={`/albums/${al.id}`} id={al.id} title={al.title} subtitle={String(al.year || "")} kind="album" />)}
          </div>
        </section>
      )}
      {comps.length > 0 && (
        <section>
          <h2 className="mb-3 text-lg font-semibold">Compilations</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            {comps.map((al) => <MediaCard key={al.id} className="max-w-none min-w-0" to={`/albums/${al.id}`} id={al.id} title={al.title} kind="album" />)}
          </div>
        </section>
      )}
    </div>
  );
}
