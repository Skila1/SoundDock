import { Heart, MoreHorizontal, Play } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent, type MouseEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Artwork } from "./Artwork";
import { Button } from "@/components/ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/misc";
import { formatDuration, artworkUrl, cn } from "@/lib/utils";
import { api } from "@/lib/api";
import type { Favourite, Playlist, Track, User } from "@/types/api";
import { toast } from "sonner";

export const TRACK_DND_MIME = "application/x-sounddock-tracks";

export type TrackChrome = Track & {
  codec?: string;
  bit_depth?: number | null;
  sample_rate?: number | null;
};

function isHires(t: TrackChrome) {
  return (t.bit_depth || 0) >= 24 && (t.sample_rate || 0) >= 48000;
}

export function downloadTrack(t: Track) {
  const a = document.createElement("a");
  a.href = `/api/v1/tracks/${t.id}/stream?quality=original`;
  a.download = `${(t.title || "track").replace(/[\\/:*?"<>|]/g, "_")}`;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

export async function addTracksToPlaylist(playlistId: string, trackIds: string[]) {
  await api.post(`/api/v1/playlists/${playlistId}/tracks`, { track_ids: trackIds });
  toast.success(trackIds.length > 1 ? `Added ${trackIds.length} tracks` : "Added to playlist");
}

export async function callWriteBack(ids: string[], artwork = false) {
  const body = { ids, write_tags: true, write_artwork: artwork, managed_only: true };
  try {
    await api.post("/api/v1/tracks/bulk/writeback", body);
    toast.success("Write-back queued");
    return true;
  } catch {
    let ok = 0;
    for (const id of ids) {
      try {
        await api.post(`/api/v1/tracks/${id}/writeback`, { write_tags: true, write_artwork: artwork, managed_only: true });
        ok++;
      } catch {
        /* P3 route not registered yet */
      }
    }
    if (ok) toast.success(`Write-back requested for ${ok} track${ok === 1 ? "" : "s"}`);
    else toast.message("Saved to library. File write-back is not wired yet.");
    return ok > 0;
  }
}

export async function saveTrackMeta(id: string, body: Record<string, unknown>) {
  try {
    await api.patch(`/api/v1/tracks/${id}/metadata`, body);
  } catch {
    if (typeof body.title === "string") await api.patch(`/api/v1/tracks/${id}`, { title: body.title });
    if (body.genre != null || body.year != null) {
      await api.post("/api/v1/tracks/bulk", { ids: [id], genre: body.genre, year: body.year });
    }
  }
}

export async function uploadArtwork(kind: "track" | "album" | "artist", id: string, file: File) {
  const fd = new FormData();
  fd.append("file", file);
  await api.post(`/api/v1/${kind}s/${id}/artwork`, fd);
}

export function TrackList({
  tracks,
  onPlay,
  onQueue,
  onNext,
  onFav,
  showAlbum = true,
  currentId,
  onSelectionChange
}: {
  tracks: TrackChrome[];
  onPlay: (index: number) => void;
  onQueue?: (t: Track) => void;
  onNext?: (t: Track) => void;
  onFav?: (t: Track) => void;
  showAlbum?: boolean;
  currentId?: string;
  onSelectionChange?: (ids: string[]) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const qc = useQueryClient();
  const virtual = tracks.length > 80;
  const rowVirtualizer = useVirtualizer({
    count: tracks.length,
    getScrollElement: () => parent.current,
    estimateSize: () => 56,
    overscan: 12,
    enabled: virtual
  });
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const anchor = useRef<number>(0);
  const [plOpen, setPlOpen] = useState(false);
  const [pendingIds, setPendingIds] = useState<string[]>([]);
  const [bulkOpen, setBulkOpen] = useState(false);
  const [bulkGenre, setBulkGenre] = useState("");
  const [bulkYear, setBulkYear] = useState("");
  const [bulkWriteBack, setBulkWriteBack] = useState(false);
  const [delOpen, setDelOpen] = useState(false);
  const [delFiles, setDelFiles] = useState(false);

  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const playlists = useQuery({
    queryKey: ["playlists"],
    queryFn: () => api.get<Playlist[]>("/api/v1/playlists"),
    enabled: plOpen
  });
  const favSet = useMemo(
    () => new Set((favs.data || []).filter((f) => f.type === "track").map((f) => f.id)),
    [favs.data]
  );
  const admin = !!me.data?.is_admin;

  useEffect(() => {
    const ids = new Set(tracks.map((t) => t.id));
    setSelected((prev) => {
      const next = new Set([...prev].filter((id) => ids.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [tracks]);

  useEffect(() => {
    onSelectionChange?.([...selected]);
  }, [selected, onSelectionChange]);

  const targetIds = (t: Track) => (selected.has(t.id) && selected.size > 1 ? [...selected] : [t.id]);

  const doFav = async (t: Track) => {
    if (t.source === "youtube") {
      toast.message("Play or queue it first so it lands in the library");
      return;
    }
    if (onFav) {
      onFav(t);
      return;
    }
    const on = !favSet.has(t.id);
    await api.post("/api/v1/favourites", { type: "track", id: t.id, on });
    qc.invalidateQueries({ queryKey: ["favourites"] });
    toast.success(on ? "Favourited" : "Removed from favourites");
  };

  const handleSelect = (t: Track, i: number, e: MouseEvent) => {
    if ((e.target as HTMLElement).closest("a,button,input")) return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (e.shiftKey) {
        const from = Math.min(anchor.current, i);
        const to = Math.max(anchor.current, i);
        const range = new Set<string>();
        for (let n = from; n <= to; n++) range.add(tracks[n].id);
        return range;
      }
      if (e.ctrlKey || e.metaKey) {
        if (next.has(t.id)) next.delete(t.id);
        else next.add(t.id);
        anchor.current = i;
        return next;
      }
      anchor.current = i;
      return new Set([t.id]);
    });
  };

  const onDragStart = (e: DragEvent, t: Track) => {
    const ids = targetIds(t);
    e.dataTransfer.setData(TRACK_DND_MIME, JSON.stringify({ track_ids: ids }));
    e.dataTransfer.setData("text/plain", ids.join(","));
    e.dataTransfer.effectAllowed = "copy";
  };

  const openPlaylist = (t: Track) => {
    setPendingIds(targetIds(t));
    setPlOpen(true);
  };

  const stopRow = (e: { stopPropagation: () => void }) => {
    e.stopPropagation();
  };

  const applyBulk = async () => {
    const ids = [...selected];
    if (!ids.length) return;
    const year = bulkYear.trim() ? Number(bulkYear) : undefined;
    const body: Record<string, unknown> = { ids };
    if (bulkGenre.trim()) body.genre = bulkGenre.trim();
    if (year && !Number.isNaN(year)) body.year = year;
    body.write_back = bulkWriteBack;
    try {
      await api.post("/api/v1/tracks/bulk/metadata", body);
    } catch {
      await api.post("/api/v1/tracks/bulk", { ids, genre: body.genre, year: body.year });
    }
      toast.success(`Queued update for ${ids.length} tracks`);
    if (bulkWriteBack) await callWriteBack(ids);
    setBulkOpen(false);
    qc.invalidateQueries({ queryKey: ["tracks"] });
  };

  const row = (t: TrackChrome, i: number, style?: CSSProperties) => (
    <ContextMenu key={t.id}>
      <ContextMenuTrigger asChild>
        <div
          style={style}
          draggable
          onDragStart={(e) => onDragStart(e, t)}
          className={cn(
            "group grid cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 hover:bg-surface-2",
            showAlbum ? "grid-cols-[32px_minmax(0,1fr)_auto_auto] md:grid-cols-[32px_minmax(0,1fr)_minmax(0,1fr)_auto_auto]" : "grid-cols-[32px_minmax(0,1fr)_auto_auto]",
            currentId === t.id && "text-accent",
            selected.has(t.id) && "bg-surface-2"
          )}
          onClick={(e) => handleSelect(t, i, e)}
          onDoubleClick={() => onPlay(i)}
        >
          <div className="relative text-center text-xs text-subtle">
            <span className="group-hover:hidden">{t.track_number || i + 1}</span>
            <button
              type="button"
              className="hidden w-full justify-center group-hover:flex"
              onPointerDown={stopRow}
              onDoubleClick={stopRow}
              onClick={(e) => {
                e.stopPropagation();
                onPlay(i);
              }}
              aria-label="Play"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
            </button>
          </div>
          <div className="flex min-w-0 items-center gap-3">
            <div className="hidden h-10 w-10 shrink-0 overflow-hidden rounded sm:block">
              <Artwork src={t.source === "youtube" ? t.artwork_url || artworkUrl("youtube", t.id, "thumb") : artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" size="sm" />
            </div>
            <div className="min-w-0">
              <div className="flex min-w-0 items-center gap-1.5">
                {t.source === "youtube" ? (
                  <span className="truncate text-sm font-medium">{t.title}</span>
                ) : (
                  <Link to={`/tracks/${t.id}`} className="truncate text-sm font-medium hover:underline" onClick={stopRow} onDoubleClick={stopRow}>
                    {t.title}
                  </Link>
                )}
                {t.source === "youtube" && <Badge>YouTube</Badge>}
                {t.explicit && <Badge tone="warning">E</Badge>}
                {t.codec && <Badge>{t.codec}</Badge>}
                {isHires(t) && <Badge tone="accent">Hi-Res</Badge>}
              </div>
              <div className="truncate text-xs text-muted">{t.artists?.map((a) => a.name).join(", ") || t.artist || ""}</div>
            </div>
          </div>
          {showAlbum && (
            <Link
              to={t.album_id ? `/albums/${t.album_id}` : "#"}
              className="hidden truncate text-sm text-muted hover:underline md:block"
              onClick={stopRow}
              onDoubleClick={stopRow}
            >
              {t.album}
            </Link>
          )}
          <div className="w-12 text-right text-xs text-subtle">{formatDuration(t.duration_ms)}</div>
          <div
            className="relative z-10 flex shrink-0 justify-end gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100"
            onPointerDown={stopRow}
            onClick={stopRow}
            onDoubleClick={stopRow}
          >
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8"
              onClick={(e) => {
                e.stopPropagation();
                doFav(t);
              }}
              aria-label="Favourite"
            >
              <Heart className={cn("h-3.5 w-3.5", favSet.has(t.id) && "fill-current text-accent")} />
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="More">
                  <MoreHorizontal className="h-3.5 w-3.5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" onClick={stopRow}>
                <DropdownMenuItem onSelect={() => onPlay(i)}>Play</DropdownMenuItem>
                {onNext && <DropdownMenuItem onSelect={() => onNext(t)}>Play next</DropdownMenuItem>}
                {onQueue && <DropdownMenuItem onSelect={() => onQueue(t)}>Add to queue</DropdownMenuItem>}
                <DropdownMenuItem onSelect={() => doFav(t)}>{favSet.has(t.id) ? "Unfavourite" : "Favourite"}</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => openPlaylist(t)}>Add to playlist</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => navigate(`/tracks/${t.id}`)}>Go to track info</DropdownMenuItem>
                {admin && t.source !== "youtube" && (
                  <DropdownMenuItem
                    onSelect={() => {
                      setSelected(new Set(targetIds(t)));
                      setDelOpen(true);
                    }}
                  >
                    Delete
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem
                  onSelect={() => {
                    targetIds(t).forEach((id) => {
                      const tr = tracks.find((x) => x.id === id) || t;
                      downloadTrack(tr);
                    });
                  }}
                >
                  Download
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onSelect={() => onPlay(i)}>Play</ContextMenuItem>
        {onNext && <ContextMenuItem onSelect={() => onNext(t)}>Play next</ContextMenuItem>}
        {onQueue && <ContextMenuItem onSelect={() => onQueue(t)}>Add to queue</ContextMenuItem>}
        <ContextMenuItem onSelect={() => doFav(t)}>{favSet.has(t.id) ? "Unfavourite" : "Favourite"}</ContextMenuItem>
        <ContextMenuItem onSelect={() => openPlaylist(t)}>Add to playlist</ContextMenuItem>
        <ContextMenuItem onSelect={() => navigate(`/tracks/${t.id}`)}>Go to track info</ContextMenuItem>
        {admin && t.source !== "youtube" && (
          <ContextMenuItem
            onSelect={() => {
              setSelected(new Set(targetIds(t)));
              setDelOpen(true);
            }}
          >
            Delete
          </ContextMenuItem>
        )}
        <ContextMenuItem
          onSelect={() => {
            targetIds(t).forEach((id) => {
              const tr = tracks.find((x) => x.id === id) || t;
              downloadTrack(tr);
            });
          }}
        >
          Download
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );

  return (
    <div>
      {selected.size > 1 && (
        <div className="mb-2 flex flex-wrap items-center gap-2 rounded-md bg-surface-2 px-3 py-2 text-sm">
          <span className="text-muted">{selected.size} selected</span>
          <Button size="sm" variant="secondary" onClick={() => { setPendingIds([...selected]); setPlOpen(true); }}>
            Add to playlist
          </Button>
          <Button size="sm" variant="secondary" onClick={() => selected.forEach((id) => { const t = tracks.find((x) => x.id === id); if (t) downloadTrack(t); })}>
            Download
          </Button>
          {admin && (
            <>
              <Button size="sm" variant="secondary" onClick={() => setBulkOpen(true)}>
                Edit metadata
              </Button>
              <Button size="sm" variant="destructive" onClick={() => setDelOpen(true)}>
                Delete selected
              </Button>
            </>
          )}
          <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>Clear</Button>
        </div>
      )}
      {!virtual ? <div>{tracks.map((t, i) => row(t, i))}</div> : (
        <div ref={parent} className="max-h-[70vh] overflow-auto scrollbar-thin">
          <div style={{ height: rowVirtualizer.getTotalSize(), position: "relative" }}>
            {rowVirtualizer.getVirtualItems().map((v) =>
              row(tracks[v.index], v.index, { position: "absolute", top: 0, left: 0, width: "100%", transform: `translateY(${v.start}px)` })
            )}
          </div>
        </div>
      )}
      <Dialog open={plOpen} onOpenChange={setPlOpen}>
        <DialogContent title="Add to playlist">
          <div className="max-h-72 space-y-1 overflow-auto">
            {(playlists.data || []).map((p) => (
              <button
                key={p.id}
                type="button"
                className="block w-full rounded-md px-2 py-2 text-left text-sm hover:bg-surface-2"
                onClick={async () => {
                  await addTracksToPlaylist(p.id, pendingIds);
                  setPlOpen(false);
                }}
              >
                {p.name}
              </button>
            ))}
            {!playlists.data?.length && !playlists.isLoading && <p className="text-sm text-muted">No playlists yet.</p>}
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={bulkOpen} onOpenChange={setBulkOpen}>
        <DialogContent title={`Edit ${selected.size} tracks`}>
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              applyBulk();
            }}
          >
            <Field label="Genre"><Input value={bulkGenre} onChange={(e) => setBulkGenre(e.target.value)} /></Field>
            <Field label="Year"><Input value={bulkYear} onChange={(e) => setBulkYear(e.target.value)} inputMode="numeric" /></Field>
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm">Write tags to files</span>
              <Switch checked={bulkWriteBack} onCheckedChange={setBulkWriteBack} />
            </div>
            <p className="text-xs text-subtle">Write-back is managed libraries only and calls P3 APIs when registered.</p>
            <div className="flex justify-end"><Button type="submit">Apply</Button></div>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={delOpen} onOpenChange={setDelOpen}>
        <DialogContent title={`Remove ${selected.size} tracks`}>
          <div className="space-y-3">
            <p className="text-sm text-muted">This removes them from SoundDock. NAS, local, and external source files are not deleted.</p>
            <label className="flex items-center justify-between gap-3 text-sm">
              Also delete SoundDock-managed files
              <Switch checked={delFiles} onCheckedChange={setDelFiles} />
            </label>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setDelOpen(false)}>Cancel</Button>
              <Button
                type="button"
                variant="destructive"
                onClick={async () => {
                  const ids = [...selected];
                  await api.post("/api/v1/tracks/bulk", { ids, delete: true, delete_files: delFiles });
                  toast.success("Delete queued");
                  setDelOpen(false);
                  setSelected(new Set());
                  qc.invalidateQueries({ queryKey: ["tracks"] });
                }}
              >
                Remove
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
