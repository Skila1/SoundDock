import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";

type CatalogAlbum = {
  id: string;
  title: string;
  year?: number | null;
  artist?: string;
  track_count?: number;
  library_id?: string;
  edition_title?: string;
};

type SearchHit = {
  id: string;
  type: string;
  title: string;
  artist?: string;
  album?: string;
  year?: number | null;
};

type TrackMeta = {
  id: string;
  title: string;
  genre?: string;
  year?: number | null;
  artist?: string;
  artists?: { name: string }[];
  album?: string;
  album_id?: string | null;
  track_number?: number;
  disc_number?: number;
};

export function AdminCatalog() {
  const qc = useQueryClient();
  const [albumQ, setAlbumQ] = useState("");
  const [singles, setSingles] = useState(true);
  const [trackQ, setTrackQ] = useState("");
  const [pickedAlbums, setPickedAlbums] = useState<Set<string>>(new Set());
  const [pickedTracks, setPickedTracks] = useState<Set<string>>(new Set());
  const [createOpen, setCreateOpen] = useState(false);
  const [newAlbum, setNewAlbum] = useState({ title: "", artist: "", year: "" });
  const [editAlbum, setEditAlbum] = useState<CatalogAlbum | null>(null);
  const [editTrack, setEditTrack] = useState<TrackMeta | null>(null);
  const [bulkGenre, setBulkGenre] = useState("");
  const [bulkYear, setBulkYear] = useState("");
  const [moveAlbumId, setMoveAlbumId] = useState("");
  const [moveNewTitle, setMoveNewTitle] = useState("");

  const albums = useQuery({
    queryKey: ["admin-catalog-albums", albumQ, singles],
    queryFn: () =>
      api.get<CatalogAlbum[]>(
        `/api/v1/albums?limit=400&singles=${singles ? "1" : "0"}&q=${encodeURIComponent(albumQ)}`
      )
  });
  const tracks = useQuery({
    queryKey: ["admin-catalog-tracks", trackQ],
    enabled: trackQ.trim().length > 0,
    queryFn: () =>
      api.get<{ results: SearchHit[] }>(
        `/api/v1/search?type=track&limit=80&q=${encodeURIComponent(trackQ.trim())}`
      )
  });
  const allAlbums = useQuery({
    queryKey: ["admin-catalog-albums-all"],
    queryFn: () => api.get<CatalogAlbum[]>("/api/v1/albums?limit=400")
  });

  const albumList = Array.isArray(albums.data) ? albums.data : [];
  const trackList = (tracks.data?.results || []).filter((h) => h.type === "track");
  const albumOptions = useMemo(
    () => (Array.isArray(allAlbums.data) ? allAlbums.data : albumList),
    [allAlbums.data, albumList]
  );

  function toggle(set: Set<string>, id: string, next: (s: Set<string>) => void) {
    const copy = new Set(set);
    if (copy.has(id)) copy.delete(id);
    else copy.add(id);
    next(copy);
  }

  async function refresh() {
    await Promise.all([
      qc.invalidateQueries({ queryKey: ["admin-catalog-albums"] }),
      qc.invalidateQueries({ queryKey: ["admin-catalog-albums-all"] }),
      qc.invalidateQueries({ queryKey: ["admin-catalog-tracks"] }),
      qc.invalidateQueries({ queryKey: ["album"] })
    ]);
  }

  async function createAlbum(trackIds: string[] = []) {
    const title = newAlbum.title.trim() || moveNewTitle.trim();
    if (!title) {
      toast.error("Album title required");
      return;
    }
    const year = newAlbum.year.trim() ? Number(newAlbum.year) : undefined;
    const created = await api.post<{ id: string }>("/api/v1/albums", {
      title,
      artist: newAlbum.artist.trim() || undefined,
      year: year && !Number.isNaN(year) ? year : undefined,
      track_ids: trackIds
    });
    toast.success("Album created");
    setCreateOpen(false);
    setNewAlbum({ title: "", artist: "", year: "" });
    setMoveNewTitle("");
    setPickedTracks(new Set());
    await refresh();
    return created?.id;
  }

  async function saveAlbum(a: CatalogAlbum, body: Record<string, unknown>) {
    await api.patch(`/api/v1/albums/${a.id}/metadata`, body);
    toast.success("Album saved");
    setEditAlbum(null);
    await refresh();
  }

  async function deleteAlbums() {
    const ids = [...pickedAlbums];
    if (!ids.length) return;
    if (!window.confirm(`Delete ${ids.length} album${ids.length === 1 ? "" : "s"}? Tracks stay in the library, unassigned.`)) {
      return;
    }
    for (const id of ids) {
      await api.del(`/api/v1/albums/${id}`);
    }
    toast.success("Albums deleted");
    setPickedAlbums(new Set());
    await refresh();
  }

  async function mergeAlbums() {
    const ids = [...pickedAlbums];
    if (ids.length < 2) {
      toast.error("Select two or more albums to merge");
      return;
    }
    const into = ids[0];
    for (const from of ids.slice(1)) {
      await api.post("/api/v1/albums/merge", { into, from });
    }
    toast.success("Albums merged");
    setPickedAlbums(new Set());
    await refresh();
  }

  async function openTrack(id: string) {
    const meta = await api.get<TrackMeta>(`/api/v1/tracks/${id}/metadata`);
    const artist = meta.artists?.map((a) => a.name).filter(Boolean).join(", ") || meta.artist || "";
    setEditTrack({ ...meta, artist });
  }

  async function saveTrack() {
    if (!editTrack) return;
    await api.patch(`/api/v1/tracks/${editTrack.id}/metadata`, {
      title: editTrack.title,
      artist: editTrack.artist,
      genre: editTrack.genre,
      year: editTrack.year,
      album_id: editTrack.album_id || "",
      track_number: editTrack.track_number,
      disc_number: editTrack.disc_number
    });
    toast.success("Track saved");
    setEditTrack(null);
    await refresh();
  }

  async function bulkTracks(extra: Record<string, unknown>) {
    const ids = [...pickedTracks];
    if (!ids.length) {
      toast.error("Select tracks first");
      return;
    }
    await api.post("/api/v1/tracks/bulk/metadata", { ids, ...extra });
    toast.success(`Updated ${ids.length} tracks`);
    setPickedTracks(new Set());
    await refresh();
  }

  async function moveSelected() {
    const ids = [...pickedTracks];
    if (!ids.length) {
      toast.error("Select tracks first");
      return;
    }
    if (moveNewTitle.trim()) {
      await createAlbum(ids);
      return;
    }
    if (!moveAlbumId) {
      toast.error("Pick an album or type a new album title");
      return;
    }
    await bulkTracks({ album_id: moveAlbumId });
  }

  return (
    <div>
      <PageHeader
        title="Catalog"
        description="Edit albums and track metadata. Autoplay uses genre and tags only - not titles. One-track albums from imports can be merged here."
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" /> New album
          </Button>
        }
      />

      <div className="grid gap-8 lg:grid-cols-2">
        <section>
          <div className="mb-3 flex flex-wrap items-center gap-3">
            <h2 className="font-semibold">Albums</h2>
            <label className="ml-auto flex items-center gap-2 text-sm text-muted">
              One-track albums
              <Switch checked={singles} onCheckedChange={setSingles} />
            </label>
          </div>
          <Input
            className="mb-3"
            placeholder="Search albums"
            value={albumQ}
            onChange={(e) => setAlbumQ(e.target.value)}
          />
          <div className="mb-3 flex flex-wrap gap-2">
            <Button size="sm" variant="secondary" disabled={pickedAlbums.size < 2} onClick={() => mergeAlbums()}>
              Merge selected
            </Button>
            <Button size="sm" variant="ghost" disabled={!pickedAlbums.size} onClick={() => deleteAlbums()}>
              <Trash2 className="h-4 w-4" /> Delete
            </Button>
          </div>
          <div className="max-h-[32rem] space-y-1 overflow-auto rounded-xl border border-border">
            {albumList.map((a) => (
              <label key={a.id} className="flex items-center gap-3 px-3 py-2 hover:bg-surface-2">
                <input
                  type="checkbox"
                  checked={pickedAlbums.has(a.id)}
                  onChange={() => toggle(pickedAlbums, a.id, setPickedAlbums)}
                />
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{a.title}</div>
                  <div className="text-xs text-muted">
                    {a.artist || "No artist"} · {a.track_count ?? 0} track{(a.track_count ?? 0) === 1 ? "" : "s"}
                    {a.year ? ` · ${a.year}` : ""}
                  </div>
                </div>
                <Button size="sm" variant="ghost" onClick={() => setEditAlbum(a)}>
                  <Pencil className="h-4 w-4" />
                </Button>
              </label>
            ))}
            {!albumList.length && (
              <p className="px-3 py-8 text-center text-sm text-muted">
                {singles ? "No one-track albums." : "No albums match."}
              </p>
            )}
          </div>
        </section>

        <section>
          <div className="mb-3 flex items-center gap-3">
            <h2 className="font-semibold">Tracks</h2>
          </div>
          <Input
            className="mb-3"
            placeholder="Search tracks to edit or move"
            value={trackQ}
            onChange={(e) => setTrackQ(e.target.value)}
          />
          <div className="mb-3 space-y-2 rounded-xl border border-border p-3">
            <p className="text-xs text-muted">Bulk: genre and tags are comma-separated. Autoplay uses these, never the title.</p>
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label="Genre / tags">
                <Input value={bulkGenre} onChange={(e) => setBulkGenre(e.target.value)} placeholder="Drum & Bass, liquid" />
              </Field>
              <Field label="Year">
                <Input value={bulkYear} onChange={(e) => setBulkYear(e.target.value)} inputMode="numeric" />
              </Field>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  bulkTracks({
                    genre: bulkGenre.trim() || undefined,
                    year: bulkYear.trim() ? Number(bulkYear) : undefined
                  })
                }
              >
                Apply to selected
              </Button>
            </div>
            <Field label="Move into album">
              <select
                className="flex h-10 w-full rounded-md border border-border bg-surface-1 px-3 text-sm"
                value={moveAlbumId}
                onChange={(e) => setMoveAlbumId(e.target.value)}
              >
                <option value="">Existing album…</option>
                {albumOptions.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.title}
                    {a.artist ? ` - ${a.artist}` : ""}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Or new album title">
              <Input value={moveNewTitle} onChange={(e) => setMoveNewTitle(e.target.value)} placeholder="Skila - Singles" />
            </Field>
            <Button size="sm" onClick={() => moveSelected()}>
              Move selected tracks
            </Button>
          </div>
          <div className="max-h-[24rem] space-y-1 overflow-auto rounded-xl border border-border">
            {trackList.map((t) => (
              <label key={t.id} className="flex items-center gap-3 px-3 py-2 hover:bg-surface-2">
                <input
                  type="checkbox"
                  checked={pickedTracks.has(t.id)}
                  onChange={() => toggle(pickedTracks, t.id, setPickedTracks)}
                />
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{t.title}</div>
                  <div className="text-xs text-muted">
                    {t.artist || "Unknown"} · {t.album || "No album"}
                  </div>
                </div>
                <Button size="sm" variant="ghost" onClick={() => openTrack(t.id)}>
                  <Pencil className="h-4 w-4" />
                </Button>
              </label>
            ))}
            {!trackQ.trim() && (
              <p className="px-3 py-8 text-center text-sm text-muted">Search to load tracks.</p>
            )}
            {trackQ.trim() && !trackList.length && !tracks.isFetching && (
              <p className="px-3 py-8 text-center text-sm text-muted">No tracks match.</p>
            )}
          </div>
        </section>
      </div>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent title="New album">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              try {
                await createAlbum();
              } catch (err) {
                toast.error(err instanceof Error ? err.message : "Could not create album");
              }
            }}
          >
            <Field label="Title">
              <Input value={newAlbum.title} onChange={(e) => setNewAlbum({ ...newAlbum, title: e.target.value })} required />
            </Field>
            <Field label="Artist">
              <Input value={newAlbum.artist} onChange={(e) => setNewAlbum({ ...newAlbum, artist: e.target.value })} />
            </Field>
            <Field label="Year">
              <Input value={newAlbum.year} onChange={(e) => setNewAlbum({ ...newAlbum, year: e.target.value })} inputMode="numeric" />
            </Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setCreateOpen(false)}>
                Cancel
              </Button>
              <Button type="submit">Create</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!editAlbum} onOpenChange={(v) => { if (!v) setEditAlbum(null); }}>
        <DialogContent title="Edit album">
          {editAlbum && (
            <AlbumEditForm
              album={editAlbum}
              onCancel={() => setEditAlbum(null)}
              onSave={(body) => saveAlbum(editAlbum, body)}
            />
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={!!editTrack} onOpenChange={(v) => { if (!v) setEditTrack(null); }}>
        <DialogContent title="Edit track">
          {editTrack && (
            <form
              className="space-y-3"
              onSubmit={async (e) => {
                e.preventDefault();
                try {
                  await saveTrack();
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : "Could not save track");
                }
              }}
            >
              <Field label="Title">
                <Input value={editTrack.title} onChange={(e) => setEditTrack({ ...editTrack, title: e.target.value })} />
              </Field>
              <Field label="Artist">
                <Input
                  value={editTrack.artist || ""}
                  onChange={(e) => setEditTrack({ ...editTrack, artist: e.target.value })}
                />
              </Field>
              <Field label="Genre / tags" hint="Comma-separated. Autoplay matches these only.">
                <Input
                  value={editTrack.genre || ""}
                  onChange={(e) => setEditTrack({ ...editTrack, genre: e.target.value })}
                />
              </Field>
              <Field label="Year">
                <Input
                  value={editTrack.year ? String(editTrack.year) : ""}
                  onChange={(e) =>
                    setEditTrack({
                      ...editTrack,
                      year: e.target.value.trim() ? Number(e.target.value) : null
                    })
                  }
                  inputMode="numeric"
                />
              </Field>
              <Field label="Album">
                <select
                  className="flex h-10 w-full rounded-md border border-border bg-surface-1 px-3 text-sm"
                  value={editTrack.album_id || ""}
                  onChange={(e) => setEditTrack({ ...editTrack, album_id: e.target.value })}
                >
                  <option value="">No album</option>
                  {albumOptions.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.title}
                    </option>
                  ))}
                </select>
              </Field>
              <div className="grid grid-cols-2 gap-2">
                <Field label="Disc">
                  <Input
                    value={String(editTrack.disc_number || 1)}
                    onChange={(e) => setEditTrack({ ...editTrack, disc_number: Number(e.target.value) || 1 })}
                    inputMode="numeric"
                  />
                </Field>
                <Field label="Track no.">
                  <Input
                    value={String(editTrack.track_number || 0)}
                    onChange={(e) => setEditTrack({ ...editTrack, track_number: Number(e.target.value) || 0 })}
                    inputMode="numeric"
                  />
                </Field>
              </div>
              <div className="flex justify-end gap-2">
                <Button type="button" variant="ghost" onClick={() => setEditTrack(null)}>
                  Cancel
                </Button>
                <Button type="submit">Save</Button>
              </div>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function AlbumEditForm({
  album,
  onCancel,
  onSave
}: {
  album: CatalogAlbum;
  onCancel: () => void;
  onSave: (body: Record<string, unknown>) => Promise<void>;
}) {
  const [title, setTitle] = useState(album.title);
  const [artist, setArtist] = useState(album.artist || "");
  const [year, setYear] = useState(album.year ? String(album.year) : "");
  const [edition, setEdition] = useState(album.edition_title || "");
  return (
    <form
      className="space-y-3"
      onSubmit={async (e) => {
        e.preventDefault();
        const y = year.trim() ? Number(year) : undefined;
        try {
          await onSave({
            title,
            artist,
            edition_title: edition,
            year: y && !Number.isNaN(y) ? y : undefined
          });
        } catch (err) {
          toast.error(err instanceof Error ? err.message : "Could not save album");
        }
      }}
    >
      <Field label="Title">
        <Input value={title} onChange={(e) => setTitle(e.target.value)} />
      </Field>
      <Field label="Artist">
        <Input value={artist} onChange={(e) => setArtist(e.target.value)} />
      </Field>
      <Field label="Year">
        <Input value={year} onChange={(e) => setYear(e.target.value)} inputMode="numeric" />
      </Field>
      <Field label="Edition">
        <Input value={edition} onChange={(e) => setEdition(e.target.value)} />
      </Field>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit">Save</Button>
      </div>
    </form>
  );
}
