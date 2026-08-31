import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { ChevronDown, ChevronUp, Copy, Heart, Pencil, Play, Radio as RadioIcon, Shuffle, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { TrackList } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import { Select } from "@/components/ui/select";
import { artworkUrl, formatDuration, relativeTime } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Playlist, Track } from "@/types/api";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { UnmatchedPanel } from "./UnmatchedPanel";
import { SyncDiffPanel } from "./SyncDiffPanel";
import type { SmartRules } from "./types";

type PlaylistDetail = Playlist & {
  folder?: string;
  is_owner?: boolean;
  can_edit?: boolean;
  is_smart?: boolean;
  snapshot_count?: number;
  smart?: { rules: SmartRules; refresh_interval_seconds?: number };
};

type SnapshotRow = { id: string; created_at: string; entry_count?: number };
type CollabRow = { id: string; username: string; display_name?: string };

export function PlaylistPage() {
  const { id } = useParams();
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const drag = useRef<number>(-1);
  const q = useQuery({
    queryKey: ["playlist", id],
    queryFn: async () => {
      const p = await api.get<PlaylistDetail>(`/api/v1/playlists/${id}`);
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
  const snaps = useQuery({
    queryKey: ["playlist-snaps", id],
    queryFn: () => api.get<SnapshotRow[]>(`/api/v1/playlists/${id}/snapshots`),
    enabled: !!id
  });
  const collabs = useQuery({
    queryKey: ["playlist-collabs", id],
    queryFn: () => api.get<CollabRow[]>(`/api/v1/playlists/${id}/collaborators`),
    enabled: !!id
  });
  const [edit, setEdit] = useState(false);
  const [del, setDel] = useState(false);
  const [smartOpen, setSmartOpen] = useState(false);
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [folder, setFolder] = useState("");
  const [isPublic, setIsPublic] = useState(false);
  const [isCollab, setIsCollab] = useState(false);
  useEffect(() => {
    if (!q.data) return;
    const { p } = q.data;
    setName(p.name);
    setDesc(p.description || "");
    setFolder(p.folder || "");
    setIsPublic(!!p.public);
    setIsCollab(!!p.collaborative);
  }, [q.data]);
  if (!q.data) return <div className="h-48 animate-pulse rounded-xl bg-surface-2" />;
  const { p, tracks } = q.data;
  const ids = tracks.map((t) => t.id);
  const total = tracks.reduce((s, t) => s + (t.duration_ms || 0), 0);
  const canEdit = p.can_edit !== false;
  const isOwner = p.is_owner !== false;

  const reorder = async (from: number, to: number) => {
    const entries = [...(p.tracks || [])];
    if (from < 0 || to < 0 || from >= entries.length || to >= entries.length) return;
    const [moved] = entries.splice(from, 1);
    entries.splice(to, 0, moved);
    await api.put(`/api/v1/playlists/${id}/tracks/order`, { order: entries.map((e) => e.entry_id) });
    toast.success("Playlist order saved");
    qc.invalidateQueries({ queryKey: ["playlist", id] });
  };

  const startRadio = () => {
    const seed = ids[0];
    if (!seed) return;
    location.href = `/radio?kind=track&seed_id=${encodeURIComponent(seed)}`;
  };

  return (
    <div>
      <div className="mb-8 flex flex-col gap-6 md:flex-row">
        <div className="h-48 w-48 overflow-hidden rounded-xl">
          <Artwork src={artworkUrl("playlist", p.id, "page")} id={p.id} name={p.name} kind="playlist" />
        </div>
        <div className="flex flex-col justify-end">
          <p className="text-xs uppercase tracking-widest text-subtle">
            {p.external ? `${p.external.provider.replace("_", " ")} playlist` : p.is_smart ? "Smart playlist" : p.folder ? `${p.folder} · Playlist` : "Playlist"}
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
            <Button
              variant="secondary"
              disabled={!ids.length}
              onClick={() => add(ids).then(() => toast.success("Queued playlist"))}
            >
              Queue
            </Button>
            <Button variant="secondary" onClick={() => play([...ids].sort(() => Math.random() - 0.5))}><Shuffle /> Shuffle</Button>
            <Button variant="secondary" onClick={startRadio} disabled={!ids.length}><RadioIcon /> Radio</Button>
            {isOwner && <Button variant="ghost" onClick={() => setEdit(true)}><Pencil /> Edit</Button>}
            <Button variant="ghost" onClick={() => api.post("/api/v1/favourites", { type: "playlist", id: p.id, on: true }).then(() => toast.success("Favourited"))}><Heart /></Button>
            {isOwner && <Button variant="ghost" onClick={() => setDel(true)}><Trash2 /></Button>}
            {p.external && (
              <Button
                variant="secondary"
                onClick={async () => {
                  await api.post(`/api/v1/playlists/${id}/external-sync`);
                  toast.success("Sync queued. Missing songs download from YouTube.");
                  qc.invalidateQueries({ queryKey: ["playlist", id] });
                  qc.invalidateQueries({ queryKey: ["unmatched", id] });
                  qc.invalidateQueries({ queryKey: ["sync-diff", id] });
                }}
              >
                Sync{p.external.provider === "spotify" ? " from Spotify" : ""}
              </Button>
            )}
            {isOwner && (
              <Button
                variant="ghost"
                onClick={async () => {
                  const r = await api.post<{ url?: string; path?: string }>(`/api/v1/playlists/${id}/invite`);
                  const link = r.url || `${location.origin}${r.path || ""}`;
                  await navigator.clipboard.writeText(link);
                  toast.success("Invite link copied");
                  qc.invalidateQueries({ queryKey: ["playlist", id] });
                }}
              >
                <Copy /> Invite
              </Button>
            )}
            {canEdit && (
              <Button
                variant="ghost"
                onClick={async () => {
                  await api.post(`/api/v1/playlists/${id}/snapshots`);
                  toast.success("Snapshot saved");
                  qc.invalidateQueries({ queryKey: ["playlist-snaps", id] });
                  qc.invalidateQueries({ queryKey: ["playlist", id] });
                }}
              >
                Snapshot
              </Button>
            )}
            {isOwner && (
              <Button variant="ghost" onClick={() => setSmartOpen(true)}>Smart rules</Button>
            )}
          </div>
        </div>
      </div>
      {p.external && <UnmatchedPanel playlistId={id!} />}
      {p.external && <SyncDiffPanel playlistId={id!} />}
      {(snaps.data?.length || collabs.data?.length) ? (
        <div className="mb-6 grid gap-4 md:grid-cols-2">
          {!!snaps.data?.length && (
            <section className="rounded-xl border border-border bg-surface-1 p-4">
              <h2 className="mb-2 font-semibold">Snapshots</h2>
              <ul className="space-y-2 text-sm">
                {snaps.data.map((s) => (
                  <li key={s.id} className="flex items-center justify-between gap-2">
                    <span>{relativeTime(s.created_at)} · {s.entry_count ?? 0} tracks</span>
                    {canEdit && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={async () => {
                          await api.post(`/api/v1/playlists/${id}/snapshots/${s.id}/restore`);
                          toast.success("Snapshot restored");
                          qc.invalidateQueries({ queryKey: ["playlist", id] });
                        }}
                      >
                        Restore
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {!!collabs.data?.length && (
            <section className="rounded-xl border border-border bg-surface-1 p-4">
              <h2 className="mb-2 font-semibold">Collaborators</h2>
              <ul className="space-y-2 text-sm">
                {collabs.data.map((c) => (
                  <li key={c.id} className="flex items-center justify-between gap-2">
                    <span>{c.display_name || c.username}</span>
                    {isOwner && (
                      <Button size="sm" variant="ghost" onClick={async () => {
                        await api.del(`/api/v1/playlists/${id}/collaborators/${c.id}`);
                        qc.invalidateQueries({ queryKey: ["playlist-collabs", id] });
                      }}>Remove</Button>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      ) : null}
      <div className="space-y-1">
        {tracks.map((t, i) => (
          <div
            key={(p.tracks?.[i]?.entry_id || t.id) + i}
            draggable={canEdit}
            onDragStart={() => { drag.current = i; }}
            onDragOver={(e) => e.preventDefault()}
            onDrop={() => canEdit && reorder(drag.current, i)}
            className="flex items-center gap-1"
          >
            {canEdit && (
              <div className="flex flex-col">
                <Button size="icon" variant="ghost" className="h-6 w-6" aria-label="Move up" onClick={() => reorder(i, i - 1)}>
                  <ChevronUp className="h-3 w-3" />
                </Button>
                <Button size="icon" variant="ghost" className="h-6 w-6" aria-label="Move down" onClick={() => reorder(i, i + 1)}>
                  <ChevronDown className="h-3 w-3" />
                </Button>
              </div>
            )}
            <div className="min-w-0 flex-1">
              <TrackList
                tracks={[t]}
                onPlay={() => play([ids[i]])}
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
              await api.put(`/api/v1/playlists/${id}`, { name, description: desc, folder, public: isPublic, collaborative: isCollab });
              toast.success("Playlist saved");
              setEdit(false);
              qc.invalidateQueries({ queryKey: ["playlist", id] });
              qc.invalidateQueries({ queryKey: ["playlists"] });
            }}
          >
            <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="Description"><Textarea value={desc} onChange={(e) => setDesc(e.target.value)} /></Field>
            <Field label="Folder"><Input value={folder} onChange={(e) => setFolder(e.target.value)} placeholder="e.g. Workouts" /></Field>
            <div className="flex items-center justify-between">
              <span className="text-sm">Public</span>
              <Switch checked={isPublic} onCheckedChange={setIsPublic} />
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm">Collaborative</span>
              <Switch checked={isCollab} onCheckedChange={setIsCollab} />
            </div>
            <div className="flex justify-end"><Button type="submit">Save</Button></div>
          </form>
        </DialogContent>
      </Dialog>
      <SmartRulesDialog
        open={smartOpen}
        onOpenChange={setSmartOpen}
        playlistId={id!}
        initial={p.smart?.rules}
        onSaved={() => {
          qc.invalidateQueries({ queryKey: ["playlist", id] });
        }}
      />
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

function SmartRulesDialog({
  open, onOpenChange, playlistId, initial, onSaved
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  playlistId: string;
  initial?: SmartRules;
  onSaved: () => void;
}) {
  const genre = (initial?.clauses || []).find((c) => c.field === "genre")?.value;
  const yearGte = (initial?.clauses || []).find((c) => c.field === "year" && c.op === "gte")?.value;
  const yearLt = (initial?.clauses || []).find((c) => c.field === "year" && (c.op === "lt" || c.op === "lte"))?.value;
  const [g, setG] = useState(genre != null ? String(genre) : "");
  const [ymin, setYmin] = useState(yearGte != null ? String(yearGte) : "");
  const [ymax, setYmax] = useState(yearLt != null ? String(yearLt) : "");
  const [limit, setLimit] = useState(String(initial?.limit || 50));
  const [sort, setSort] = useState(initial?.sort || "random");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Smart playlist">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const clauses: SmartRules["clauses"] = [];
            if (g) clauses!.push({ field: "genre", op: "eq", value: g });
            if (ymin) clauses!.push({ field: "year", op: "gte", value: Number(ymin) });
            if (ymax) clauses!.push({ field: "year", op: "lt", value: Number(ymax) });
            await api.put(`/api/v1/playlists/${playlistId}/smart`, {
              rules: { limit: Number(limit) || 50, match: "all", sort, clauses },
              refresh_interval_seconds: 86400
            });
            toast.success("Smart rules saved · refresh queued");
            onOpenChange(false);
            onSaved();
          }}
        >
          <Field label="Genre"><Input value={g} onChange={(e) => setG(e.target.value)} placeholder="Rock" /></Field>
          <div className="grid grid-cols-2 gap-2">
            <Field label="Year from"><Input value={ymin} onChange={(e) => setYmin(e.target.value)} inputMode="numeric" /></Field>
            <Field label="Year to"><Input value={ymax} onChange={(e) => setYmax(e.target.value)} inputMode="numeric" /></Field>
          </div>
          <Field label="Limit"><Input value={limit} onChange={(e) => setLimit(e.target.value)} inputMode="numeric" /></Field>
          <Field label="Sort">
            <Select value={sort} onValueChange={setSort} options={[
              { value: "random", label: "Random" },
              { value: "recent", label: "Recently added" },
              { value: "title", label: "Title" },
              { value: "year", label: "Year" },
              { value: "most_played", label: "Most played" }
            ]} />
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={async () => {
              await api.post(`/api/v1/playlists/${playlistId}/smart/refresh`);
              toast.success("smart_playlist.refresh queued");
            }}>Refresh now</Button>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
