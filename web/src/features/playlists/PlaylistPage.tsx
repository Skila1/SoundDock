import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { ChevronDown, ChevronUp, Heart, Pencil, Play, Shuffle, Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { TrackList } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { artworkUrl, formatDuration } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Playlist, Track } from "@/types/api";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { UnmatchedPanel } from "./UnmatchedPanel";

export function PlaylistPage() {
  const { id } = useParams();
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const drag = useRef<number>(-1);
  const q = useQuery({
    queryKey: ["playlist", id],
    queryFn: async () => {
      const p = await api.get<Playlist>(`/api/v1/playlists/${id}`);
      const tracks: Track[] = [];
      for (const t of p.tracks || []) {
        try {
          tracks.push(await api.get<Track>(`/api/v1/tracks/${t.track_id}`));
        } catch {
          tracks.push({ id: t.track_id, title: t.title });
        }
      }
      return { p, tracks };
    }
  });
  const [edit, setEdit] = useState(false);
  const [del, setDel] = useState(false);
  if (!q.data) return <div className="h-48 animate-pulse rounded-xl bg-surface-2" />;
  const { p, tracks } = q.data;
  const ids = tracks.map((t) => t.id);
  const total = tracks.reduce((s, t) => s + (t.duration_ms || 0), 0);
  const [name, setName] = useState(p.name);
  const [desc, setDesc] = useState(p.description || "");

  const reorder = async (from: number, to: number) => {
    const entries = [...(p.tracks || [])];
    if (from < 0 || to < 0 || from >= entries.length || to >= entries.length) return;
    const [moved] = entries.splice(from, 1);
    entries.splice(to, 0, moved);
    await api.put(`/api/v1/playlists/${id}/tracks/order`, { order: entries.map((e) => e.entry_id) });
    toast.success("Playlist order saved");
    qc.invalidateQueries({ queryKey: ["playlist", id] });
  };

  return (
    <div>
      <div className="mb-8 flex flex-col gap-6 md:flex-row">
        <div className="h-48 w-48 overflow-hidden rounded-xl">
          <Artwork src={artworkUrl("playlist", p.id, "page")} id={p.id} name={p.name} kind="playlist" />
        </div>
        <div className="flex flex-col justify-end">
          <p className="text-xs uppercase tracking-widest text-subtle">
            {p.external ? `${p.external.provider.replace("_", " ")} playlist` : "Playlist"}
          </p>
          <h1 className="text-4xl font-semibold">{p.name}</h1>
          {p.description && <p className="mt-1 text-muted">{p.description}</p>}
          <p className="text-sm text-subtle">
            {tracks.length} playable
            {p.external ? ` · ${p.external.matched} of ${p.external.matched + p.external.unmatched} in your library` : ""}
            {" · "}{formatDuration(total)}
          </p>
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={() => play(ids)} disabled={!ids.length}><Play className="fill-current" /> Play</Button>
            <Button variant="secondary" onClick={() => play([...ids].sort(() => Math.random() - 0.5))}><Shuffle /> Shuffle</Button>
            <Button variant="ghost" onClick={() => setEdit(true)}><Pencil /> Edit</Button>
            <Button variant="ghost" onClick={() => api.post("/api/v1/favourites", { type: "playlist", id: p.id, on: true }).then(() => toast.success("Favourited"))}><Heart /></Button>
            <Button variant="ghost" onClick={() => setDel(true)}><Trash2 /></Button>
            {p.external && (
              <Button
                variant="secondary"
                onClick={async () => {
                  await api.post(`/api/v1/playlists/${id}/external-sync`);
                  toast.success("Sync queued");
                  qc.invalidateQueries({ queryKey: ["playlist", id] });
                  qc.invalidateQueries({ queryKey: ["unmatched", id] });
                }}
              >
                Sync now
              </Button>
            )}
          </div>
        </div>
      </div>
      {p.external && <UnmatchedPanel playlistId={id!} />}
      <div className="space-y-1">
        {tracks.map((t, i) => (
          <div
            key={(p.tracks?.[i]?.entry_id || t.id) + i}
            draggable
            onDragStart={() => { drag.current = i; }}
            onDragOver={(e) => e.preventDefault()}
            onDrop={() => reorder(drag.current, i)}
            className="flex items-center gap-1"
          >
            <div className="flex flex-col">
              <Button size="icon" variant="ghost" className="h-6 w-6" aria-label="Move up" onClick={() => reorder(i, i - 1)}>
                <ChevronUp className="h-3 w-3" />
              </Button>
              <Button size="icon" variant="ghost" className="h-6 w-6" aria-label="Move down" onClick={() => reorder(i, i + 1)}>
                <ChevronDown className="h-3 w-3" />
              </Button>
            </div>
            <div className="min-w-0 flex-1">
              <TrackList
                tracks={[t]}
                onPlay={() => play(ids, i)}
                onQueue={(tr) => add([tr.id])}
                onNext={(tr) => add([tr.id], true)}
              />
            </div>
          </div>
        ))}
      </div>
      <Dialog open={edit} onOpenChange={setEdit}>
        <DialogContent title="Edit playlist">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await api.put(`/api/v1/playlists/${id}`, { name, description: desc });
              toast.success("Playlist saved");
              setEdit(false);
              qc.invalidateQueries({ queryKey: ["playlist", id] });
            }}
          >
            <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="Description"><Textarea value={desc} onChange={(e) => setDesc(e.target.value)} /></Field>
            <div className="flex justify-end"><Button type="submit">Save</Button></div>
          </form>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={del}
        onOpenChange={setDel}
        title="Delete playlist?"
        description="This cannot be undone."
        confirmLabel="Delete"
        destructive
        onConfirm={async () => {
          await api.del(`/api/v1/playlists/${id}`);
          toast.success("Playlist deleted");
          location.href = "/playlists";
        }}
      />
    </div>
  );
}
