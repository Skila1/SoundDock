import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { Heart, Pencil, Play, Shuffle } from "lucide-react";
import { useState } from "react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { CoverEditor } from "@/components/media/CoverEditor";
import { hasPerm } from "@/lib/perms";
import { MediaCard } from "@/components/media/MediaCard";
import { TrackList } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { artworkUrl } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Artist, Favourite, User } from "@/types/api";
import { toast } from "sonner";

export function ArtistPage() {
  const { id } = useParams();
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const q = useQuery({ queryKey: ["artist", id], queryFn: () => api.get<Artist>(`/api/v1/artists/${id}`) });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const [edit, setEdit] = useState(false);
  const a = q.data;
  if (!a) return <div className="h-48 animate-pulse rounded-xl bg-surface-2" />;
  const albums = (a.albums || []).filter((x) => !x.is_compilation);
  const comps = (a.albums || []).filter((x) => x.is_compilation);
  const ids = (a.tracks || []).map((t) => t.id);
  const fav = !!(favs.data || []).some((f) => f.type === "artist" && f.id === a.id);
  const admin = !!me.data?.is_admin;
  const canEditCover = admin || hasPerm(me.data, "library.upload");

  const toggleFav = async () => {
    await api.post("/api/v1/favourites", { type: "artist", id: a.id, on: !fav });
    qc.invalidateQueries({ queryKey: ["favourites"] });
    toast.success(fav ? "Removed from favourites" : "Favourited");
  };

  return (
    <div>
      <div className="mb-8 flex flex-col gap-6 md:flex-row md:items-end">
        <CoverEditor kind="artist" id={a.id} canEdit={canEditCover} className="h-40 w-40 rounded-full shadow-card md:h-52 md:w-52">
          <Artwork src={artworkUrl("artist", a.id, "page")} id={a.id} name={a.name} kind="artist" />
        </CoverEditor>
        <div>
          <p className="text-xs uppercase tracking-widest text-subtle">Artist</p>
          <h1 className="text-4xl font-semibold md:text-6xl">{a.name}</h1>
          <p className="mt-2 text-sm text-muted">{albums.length} albums · {(a.tracks || []).length} tracks</p>
          <div className="mt-4 flex gap-2">
            <Button disabled={!ids.length} onClick={() => play(ids)}><Play className="fill-current" /> Play</Button>
            <Button variant="secondary" disabled={!ids.length} onClick={() => play([...ids].sort(() => Math.random() - 0.5))}><Shuffle /> Shuffle</Button>
            <Button variant="ghost" onClick={toggleFav} aria-label="Favourite"><Heart className={fav ? "fill-current" : ""} /></Button>
            {admin && <Button variant="ghost" onClick={() => setEdit(true)}><Pencil /> Edit</Button>}
          </div>
        </div>
      </div>
      {(a.tracks || []).length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 text-lg font-semibold">Popular</h2>
          <TrackList
            tracks={a.tracks || []}
            onPlay={(i) => play([ids[i]])}
            onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
            onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
            showAlbum
          />
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
      {edit && (
        <ArtistEditDialog
          artist={a}
          onClose={() => setEdit(false)}
          onSaved={() => { setEdit(false); qc.invalidateQueries({ queryKey: ["artist", id] }); }}
        />
      )}
    </div>
  );
}

function ArtistEditDialog({ artist, onClose, onSaved }: { artist: Artist; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(artist.name);
  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title="Edit artist">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await api.patch(`/api/v1/artists/${artist.id}/metadata`, { name });
              toast.success("Artist saved");
              onSaved();
            } catch {
              toast.error("Artist metadata route is not registered yet");
            }
          }}
        >
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <p className="text-xs text-subtle">Click the photo to replace the artist image.</p>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
