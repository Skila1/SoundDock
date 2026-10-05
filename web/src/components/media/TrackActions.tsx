import { useState, type ComponentType, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { create } from "zustand";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { isLibraryTrackId } from "@/lib/utils";
import { refreshCatalogue, removeTracksFromCaches } from "@/lib/catalogue";
import { saveTracksOffline } from "@/lib/offlineFill";
import { usePlayer } from "@/stores/player";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { TrackEditDialog } from "./TrackEditDialog";
import { MediaCard } from "./MediaCard";
import { addTracksToPlaylist, downloadTrack } from "./TrackList";
import type { Favourite, Playlist, Track, User } from "@/types/api";

type ActionsState = {
  editId: string | null;
  playlistIds: string[] | null;
  deleteIds: string[] | null;
  openEdit: (id: string) => void;
  openPlaylist: (ids: string[]) => void;
  openDelete: (ids: string[]) => void;
  close: () => void;
};

/** App-wide track dialogs, so any menu (list rows, grid cards, pages) can open them. */
export const useTrackActions = create<ActionsState>((set) => ({
  editId: null,
  playlistIds: null,
  deleteIds: null,
  openEdit: (id) => set({ editId: id }),
  openPlaylist: (ids) => set({ playlistIds: ids }),
  openDelete: (ids) => set({ deleteIds: ids.filter((id) => isLibraryTrackId(id)) }),
  close: () => set({ editId: null, playlistIds: null, deleteIds: null })
}));

const songCount = (n: number) => (n === 1 ? "1 song" : `${n} songs`);

/** Mount once (AppShell). Renders whichever shared track dialog is open. */
export function TrackActionsHost() {
  const s = useTrackActions();
  return (
    <>
      {s.editId && <TrackEditDialog trackId={s.editId} onClose={() => useTrackActions.setState({ editId: null })} />}
      <PlaylistPicker ids={s.playlistIds} onClose={() => useTrackActions.setState({ playlistIds: null })} />
      <DeleteTracks ids={s.deleteIds} onClose={() => useTrackActions.setState({ deleteIds: null })} />
    </>
  );
}

function PlaylistPicker({ ids, onClose }: { ids: string[] | null; onClose: () => void }) {
  const playlists = useQuery({
    queryKey: ["playlists"],
    queryFn: () => api.get<Playlist[]>("/api/v1/playlists"),
    enabled: !!ids
  });
  return (
    <Dialog open={!!ids} onOpenChange={(v) => !v && onClose()}>
      <DialogContent title="Add to playlist">
        <div className="max-h-72 space-y-1 overflow-auto scrollbar-thin">
          {(playlists.data || []).map((p) => (
            <button
              key={p.id}
              type="button"
              className="block w-full rounded-md px-2 py-2 text-left text-sm hover:bg-surface-2"
              onClick={async () => {
                try {
                  await addTracksToPlaylist(p.id, ids || []);
                  onClose();
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : "Could not add to playlist");
                }
              }}
            >
              {p.name}
            </button>
          ))}
          {playlists.isLoading && <p className="px-2 text-sm text-muted">Loading playlists…</p>}
          {!playlists.data?.length && !playlists.isLoading && <p className="text-sm text-muted">No playlists yet.</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DeleteTracks({ ids, onClose }: { ids: string[] | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [files, setFiles] = useState(false);
  const [busy, setBusy] = useState(false);
  const n = ids?.length || 0;
  return (
    <Dialog open={!!ids} onOpenChange={(v) => !v && !busy && onClose()}>
      <DialogContent title={`Delete ${songCount(n)}`}>
        <div className="space-y-3">
          <p className="text-sm text-muted">
            This removes them from SoundDock for everyone, including playlists and personal libraries. NAS, local, and external source files are not deleted.
          </p>
          <label className="flex items-center justify-between gap-3 text-sm">
            Also delete SoundDock-managed files
            <Switch checked={files} onCheckedChange={setFiles} />
          </label>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={busy || !n}
              onClick={async () => {
                if (!ids?.length) return;
                setBusy(true);
                try {
                  const res = await api.post<{ deleted?: number; skipped?: unknown[] }>("/api/v1/tracks/bulk", { ids, delete: true, delete_files: files });
                  const skipped = Array.isArray(res?.skipped) ? res.skipped.length : 0;
                  removeTracksFromCaches(qc, ids);
                  toast.success(`Deleted ${songCount(res?.deleted ?? ids.length)}` + (skipped ? `. ${skipped} could not be deleted (in use or locked).` : ""));
                  refreshCatalogue(qc);
                  void qc.invalidateQueries({ queryKey: ["personal-library"] });
                  onClose();
                } catch (err) {
                  toast.error(err instanceof Error ? `Could not delete: ${err.message}` : "Could not delete songs");
                } finally {
                  setBusy(false);
                }
              }}
            >
              {busy ? "Deleting…" : "Delete"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

type ItemProps = { onSelect?: (e: Event) => void; children: ReactNode; className?: string };

/**
 * The standard song menu. Pass the menu flavour's Item (context or dropdown) so
 * right-click and the "more" button always offer the same actions.
 */
export function TrackMenuItems({
  track,
  ids,
  Item,
  Separator,
  onPlay
}: {
  track: Track;
  /** Selected ids the action applies to; defaults to just this track. */
  ids?: string[];
  Item: ComponentType<ItemProps>;
  Separator?: ComponentType;
  onPlay?: () => void;
}) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const actions = useTrackActions.getState();
  const targets = ids?.length ? ids : [track.id];
  const youtube = track.source === "youtube";
  const fav = (favs.data || []).some((f) => f.type === "track" && f.id === track.id);
  const admin = !!me.data?.is_admin;
  const toggleFav = async () => {
    if (youtube) {
      toast.message("Play or queue it first so it lands in the library");
      return;
    }
    try {
      await api.post("/api/v1/favourites", { type: "track", id: track.id, on: !fav });
      void qc.invalidateQueries({ queryKey: ["favourites"] });
      toast.success(fav ? "Removed from favourites" : "Favourited");
    } catch {
      toast.error("Could not update favourites");
    }
  };
  return (
    <>
      <Item onSelect={() => (onPlay ? onPlay() : void play(targets))}>Play</Item>
      <Item onSelect={() => void add(targets, true).then(() => toast.success("Playing next"))}>Play next</Item>
      <Item onSelect={() => void add(targets).then(() => toast.success("Added to queue"))}>Add to queue</Item>
      {Separator && <Separator />}
      <Item onSelect={() => void toggleFav()}>{fav ? "Unfavourite" : "Favourite"}</Item>
      <Item onSelect={() => actions.openPlaylist(targets)}>Add to playlist</Item>
      <Item onSelect={() => saveTracksOffline(targets)}>Save offline</Item>
      {!youtube && <Item onSelect={() => navigate(`/tracks/${track.id}`)}>Go to track info</Item>}
      {track.album_id && <Item onSelect={() => navigate(`/albums/${track.album_id}`)}>Go to album</Item>}
      <Item onSelect={() => downloadTrack(track)}>Download</Item>
      {admin && !youtube && (
        <>
          {Separator && <Separator />}
          <Item onSelect={() => actions.openEdit(track.id)}>Edit…</Item>
          <Item className="text-destructive" onSelect={() => actions.openDelete(targets)}>
            Delete
          </Item>
        </>
      )}
    </>
  );
}

/** A grid card for a song with the same right-click menu as list rows. */
export function TrackCard({ track, onPlay, subtitle }: { track: Track; onPlay: () => void; subtitle?: string }) {
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div className="min-w-0">
          <MediaCard
            className="w-full min-w-0 max-w-none"
            to={`/tracks/${track.id}`}
            id={track.id}
            title={track.title}
            subtitle={subtitle ?? (track.artist || track.album)}
            kind="track"
            explicit={track.explicit}
            onPlay={onPlay}
          />
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <TrackMenuItems track={track} Item={ContextMenuItem} Separator={ContextMenuSeparator} onPlay={onPlay} />
      </ContextMenuContent>
    </ContextMenu>
  );
}
