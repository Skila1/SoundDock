import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { Heart, ListPlus, Pencil, Play, Shuffle } from "lucide-react";
import { useRef, useState } from "react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { TrackList, callWriteBack, uploadArtwork } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { artworkUrl, formatDuration } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Album, Favourite, Track, User } from "@/types/api";
import { toast } from "sonner";

export function AlbumPage() {
  const { id } = useParams();
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const fileRef = useRef<HTMLInputElement>(null);
  const q = useQuery({ queryKey: ["album", id], queryFn: () => api.get<Album>(`/api/v1/albums/${id}`) });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const [edit, setEdit] = useState(false);
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
  const fav = !!(favs.data || []).some((f) => f.type === "album" && f.id === a.id);
  const admin = !!me.data?.is_admin;

  const toggleFav = async () => {
    await api.post("/api/v1/favourites", { type: "album", id: a.id, on: !fav });
    qc.invalidateQueries({ queryKey: ["favourites"] });
    toast.success(fav ? "Removed from favourites" : "Favourited");
  };

  return (
    <div>
      <div className="mb-8 flex flex-col gap-6 md:flex-row">
        <button type="button" className="h-52 w-52 overflow-hidden rounded-xl shadow-card" onClick={() => admin && fileRef.current?.click()} aria-label="Album artwork">
          <Artwork src={artworkUrl("album", a.id, "page")} id={a.id} name={a.title} kind="album" />
        </button>
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={async (e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (!file || !id) return;
            try {
              await uploadArtwork("album", id, file);
              toast.success("Artwork saved");
              qc.invalidateQueries({ queryKey: ["album", id] });
            } catch {
              toast.error("Artwork upload is not available yet");
            }
          }}
        />
        <div className="flex flex-col justify-end">
          <p className="text-xs uppercase tracking-widest text-subtle">{a.is_compilation ? "Compilation" : "Album"}</p>
          <h1 className="text-4xl font-semibold md:text-5xl">{a.title}</h1>
          <p className="mt-2 text-muted">{a.artist}{a.edition_title ? ` · ${a.edition_title}` : ""}{a.year ? ` · ${a.year}` : ""}</p>
          <p className="text-sm text-subtle">{tracks.length} tracks · {formatDuration(total)}</p>
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={() => play(ids)}><Play className="fill-current" /> Play</Button>
            <Button variant="secondary" onClick={() => play([...ids].sort(() => Math.random() - 0.5))}><Shuffle /> Shuffle</Button>
            <Button variant="ghost" onClick={toggleFav} aria-label="Favourite"><Heart className={fav ? "fill-current" : ""} /></Button>
            <Button variant="ghost" onClick={() => add(ids).then(() => toast.success("Added to queue"))}><ListPlus /> Add to queue</Button>
            {admin && <Button variant="ghost" onClick={() => setEdit(true)}><Pencil /> Edit</Button>}
          </div>
        </div>
      </div>
      {[...discs.entries()].map(([disc, list]) => (
        <section key={disc} className="mb-6">
          {discs.size > 1 && <h2 className="mb-2 text-sm font-semibold text-muted">Disc {disc}</h2>}
          <TrackList
            tracks={list}
            showAlbum={false}
            onPlay={(i) => play([list[i].id])}
            onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
            onNext={(t) => add([t.id], true)}
          />
        </section>
      ))}
      {edit && <AlbumEditDialog album={a} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); qc.invalidateQueries({ queryKey: ["album", id] }); }} />}
    </div>
  );
}

function AlbumEditDialog({ album, onClose, onSaved }: { album: Album; onClose: () => void; onSaved: () => void }) {
  const [title, setTitle] = useState(album.title);
  const [year, setYear] = useState(album.year ? String(album.year) : "");
  const [edition, setEdition] = useState(album.edition_title || "");
  const [writeBack, setWriteBack] = useState(false);
  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title="Edit album">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const y = year.trim() ? Number(year) : undefined;
            const body = { title, year: y && !Number.isNaN(y) ? y : undefined, edition_title: edition, write_back: writeBack };
            try {
              await api.patch(`/api/v1/albums/${album.id}/metadata`, body);
            } catch {
              await api.patch(`/api/v1/albums/${album.id}`, { title, edition: edition });
            }
            toast.success("Album saved");
            if (writeBack && album.tracks?.length) await callWriteBack(album.tracks.map((t) => t.id), true);
            onSaved();
          }}
        >
          <Field label="Title"><Input value={title} onChange={(e) => setTitle(e.target.value)} /></Field>
          <Field label="Year"><Input value={year} onChange={(e) => setYear(e.target.value)} inputMode="numeric" /></Field>
          <Field label="Edition"><Input value={edition} onChange={(e) => setEdition(e.target.value)} /></Field>
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm">Write tags to files</span>
            <Switch checked={writeBack} onCheckedChange={setWriteBack} />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
