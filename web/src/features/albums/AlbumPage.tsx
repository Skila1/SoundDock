import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { Heart, ListPlus, Pencil, Shuffle, Trash2 } from "lucide-react";
import { useState } from "react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { HeroIconButton, MediaHero } from "@/components/media/MediaHero";
import { CoverEditor } from "@/components/media/CoverEditor";
import { hasPerm } from "@/lib/perms";
import { TrackList, callWriteBack } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { refreshCatalogue, removeTracksFromCaches } from "@/lib/catalogue";
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
  const q = useQuery({ queryKey: ["album", id], queryFn: () => api.get<Album>(`/api/v1/albums/${id}`) });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const [edit, setEdit] = useState(false);
  const [delOpen, setDelOpen] = useState(false);
  const navigate = useNavigate();
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
  const canEditCover = admin || hasPerm(me.data, "library.upload");

  const toggleFav = async () => {
    await api.post("/api/v1/favourites", { type: "album", id: a.id, on: !fav });
    qc.invalidateQueries({ predicate: (q) => q.queryKey[0] === "favourites" || q.queryKey[0] === "home" });
    toast.success(fav ? "Removed from favourites" : "Favourited");
  };

  return (
    <div>
      <MediaHero
        art={
          <CoverEditor kind="album" id={a.id} canEdit={canEditCover} className="h-full w-full">
            <Artwork src={artworkUrl("album", a.id, "page")} id={a.id} name={a.title} kind="album" />
          </CoverEditor>
        }
        backdrop={artworkUrl("album", a.id, "thumb")}
        eyebrow={a.is_compilation ? "Compilation" : "Album"}
        title={a.title}
        meta={
          <>
            <span className="font-semibold">{a.artist}</span>
            {a.edition_title && <span className="text-muted">· {a.edition_title}</span>}
            {a.year && <span className="text-muted">· {a.year}</span>}
          </>
        }
        stats={`${tracks.length} ${tracks.length === 1 ? "song" : "songs"} · ${formatDuration(total)}`}
        onPlay={() => play(ids)}
        playDisabled={!ids.length}
        actions={
          <>
            <HeroIconButton label="Shuffle" onClick={() => play([...ids].sort(() => Math.random() - 0.5))} disabled={!ids.length}><Shuffle /></HeroIconButton>
            <HeroIconButton label={fav ? "Remove from favourites" : "Favourite"} active={fav} onClick={toggleFav}><Heart className={fav ? "fill-current" : ""} /></HeroIconButton>
            <HeroIconButton label="Add to queue" onClick={() => add(ids).then(() => toast.success("Added to queue"))} disabled={!ids.length}><ListPlus /></HeroIconButton>
          </>
        }
        menu={[
          { label: "Play next", icon: <ListPlus className="h-4 w-4" />, onSelect: () => void add(ids, true).then(() => toast.success("Playing next")), hidden: !ids.length },
          { label: "Edit album", icon: <Pencil className="h-4 w-4" />, onSelect: () => setEdit(true), hidden: !admin },
          "separator",
          { label: "Delete album", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDelOpen(true), destructive: true, hidden: !admin || !ids.length }
        ]}
      />
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
      <ConfirmDialog
        open={delOpen}
        onOpenChange={setDelOpen}
        title={`Delete "${a.title}"?`}
        description={`This deletes all ${tracks.length} songs on this album from SoundDock for everyone. Source files on NAS or local libraries are not deleted.`}
        confirmLabel="Delete album"
        destructive
        onConfirm={async () => {
          try {
            const res = await api.post<{ deleted?: number; skipped?: unknown[] }>("/api/v1/tracks/bulk", { ids, delete: true });
            removeTracksFromCaches(qc, ids);
            refreshCatalogue(qc);
            const skipped = Array.isArray(res?.skipped) ? res.skipped.length : 0;
            toast.success(skipped ? `Album deleted. ${skipped} songs could not be deleted.` : "Album deleted");
            navigate("/library/albums");
          } catch (err) {
            toast.error(err instanceof Error ? `Could not delete: ${err.message}` : "Could not delete album");
          }
        }}
      />
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
