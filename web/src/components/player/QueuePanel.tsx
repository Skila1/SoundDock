import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Download, GripVertical, History, ListMusic, ListPlus, MoreHorizontal, PanelRightClose, Pin, PinOff, Play, Trash2, Undo2, X } from "lucide-react";
import { fillableTrackIds, saveTracksOffline } from "@/lib/offlineFill";
import { addTracksToPlaylist, downloadTrack } from "@/components/media/TrackList";
import { Button } from "@/components/ui/button";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl, cn, isLibraryTrackId, relativeTime } from "@/lib/utils";
import { usePlayer, type PlayerQueueItem, type RequestedBy } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Favourite, Playlist, Track, User } from "@/types/api";
import { Switch } from "@/components/ui/switch";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Tooltip } from "@/components/ui/tooltip";
import { asListenTracks, type ListenTrack } from "@/features/stats/types";
import type { PresenceParticipant, PresenceSource } from "@/stores/sseClient";
import { SoftBoundary } from "@/app/ErrorBoundary";
import { avatarDisplaySrc, pageIsActive } from "./presenceAvatar";
import { refreshCatalogue, removeTracksFromCaches } from "@/lib/catalogue";
import { toast } from "sonner";

function usePageActive() {
  const [active, setActive] = useState(pageIsActive);
  useEffect(() => {
    const sync = () => setActive(pageIsActive());
    document.addEventListener("visibilitychange", sync);
    window.addEventListener("focus", sync);
    window.addEventListener("blur", sync);
    sync();
    return () => {
      document.removeEventListener("visibilitychange", sync);
      window.removeEventListener("focus", sync);
      window.removeEventListener("blur", sync);
    };
  }, []);
  return active;
}

function PresenceAvatar({ src, active }: { src: string; active: boolean }) {
  return (
    <img
      src={avatarDisplaySrc(src, active)}
      alt=""
      referrerPolicy="no-referrer"
      className="h-7 w-7 rounded-full object-cover ring-2 ring-surface-1"
      decoding={active ? "async" : "sync"}
    />
  );
}

const MAX_VISIBLE_AVATARS = 4;

export function addedByLabel(
  requested: RequestedBy | undefined,
  meId: string | undefined,
  listeners: PresenceParticipant[]
): string | null {
  if (!requested) return null;
  const uid = requested.user_id;
  if (uid && meId && uid === meId) return "Added by You";
  const fromPresence = uid ? listeners.find((p) => p.user_id === uid) : undefined;
  const name = requested.display_name || fromPresence?.display_name;
  if (!name) return null;
  return `Added by ${name}`;
}

export function presenceLabel(p: Pick<PresenceParticipant, "display_name"> | null | undefined): string {
  const n = typeof p?.display_name === "string" ? p.display_name.trim() : "";
  return n || "Listener";
}

export function orderPresence(listeners: PresenceParticipant[] | null | undefined, meId?: string): PresenceParticipant[] {
  const rows = (listeners || []).filter((p) => p && (p.user_id || p.display_name));
  return [...rows].sort((a, b) => {
    const aSelf = !!meId && a.user_id === meId;
    const bSelf = !!meId && b.user_id === meId;
    if (aSelf !== bSelf) return aSelf ? -1 : 1;
    return presenceLabel(a).localeCompare(presenceLabel(b), undefined, { sensitivity: "base" });
  });
}

function sourceLabel(source: PresenceSource) {
  if (source === "discord") return "Discord";
  if (source === "both") return "Web + Discord";
  return "Web";
}

function sourcePip(source: PresenceSource) {
  if (source === "discord") return "bg-[#5865F2]";
  if (source === "both") return "bg-gradient-to-r from-accent to-[#5865F2]";
  return "bg-accent";
}

export function QueuePresence({ className }: { className?: string }) {
  const listeners = usePlayer((s) => s.listeners);
  const pageActive = usePageActive();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const ordered = orderPresence(listeners, me.data?.id);
  if (!ordered.length) return null;
  const shown = ordered.slice(0, MAX_VISIBLE_AVATARS);
  const overflow = ordered.slice(MAX_VISIBLE_AVATARS);

  return (
    <SoftBoundary>
      <div className={cn("flex min-w-0 items-center", className)} aria-label="Who is here" role="group">
        <div className="flex items-center -space-x-2">
          {shown.map((p, i) => {
            const self = p.user_id === me.data?.id;
            const name = presenceLabel(p);
            const label = `${name}${self ? " (you)" : ""} · ${sourceLabel(p.source)}`;
            return (
              <Tooltip key={p.user_id || name || String(i)} label={label}>
                <span className="relative inline-flex h-7 w-7 shrink-0">
                  {p.avatar_url ? (
                    <PresenceAvatar src={p.avatar_url} active={pageActive} />
                  ) : (
                    <span className="flex h-7 w-7 items-center justify-center rounded-full bg-surface-3 text-[10px] font-semibold text-foreground ring-2 ring-surface-1">
                      {name.slice(0, 1).toUpperCase()}
                    </span>
                  )}
                  <span
                    className={cn("absolute bottom-0 right-0 h-2 w-2 rounded-full ring-1 ring-surface-1", sourcePip(p.source))}
                    aria-hidden
                  />
                </span>
              </Tooltip>
            );
          })}
        </div>
        {overflow.length > 0 && (
          <Tooltip label={overflow.map((p) => `${presenceLabel(p)} · ${sourceLabel(p.source)}`).join(", ")}>
            <span className="ml-1 rounded-full bg-surface-2 px-1.5 py-0.5 text-[10px] font-semibold text-muted">+{overflow.length}</span>
          </Tooltip>
        )}
      </div>
    </SoftBoundary>
  );
}

export function showQueueInsertLine(from: number, over: number | null, at: number): boolean {
  if (from < 0 || over == null || over < 0) return false;
  if (from === over) return false;
  return over === at;
}

export function queueReorderTarget(from: number, over: number, length: number): number | null {
  if (from < 0 || length <= 0 || over < 0) return null;
  const to = over >= length ? length - 1 : over;
  if (to < 0 || from === to) return null;
  return to;
}

function QueueInsertLine({ active }: { active: boolean }) {
  return (
    <div
      className="overflow-hidden transition-[height,opacity] duration-150 ease-out"
      style={{ height: active ? 8 : 0, opacity: active ? 1 : 0 }}
      aria-hidden
    >
      <div className="mx-2 h-2 rounded-full bg-accent shadow-[0_0_10px] shadow-accent/40" />
    </div>
  );
}

export function QueuePanel({
  onClose,
  onCollapse,
  showPresence = true
}: {
  onClose?: () => void;
  onCollapse?: () => void;
  showPresence?: boolean;
}) {
  const p = usePlayer();
  const ui = useUi();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [dragFrom, setDragFrom] = useState(-1);
  const [overIndex, setOverIndex] = useState<number | null>(null);
  const [flashId, setFlashId] = useState<string | null>(null);
  const [saveOpen, setSaveOpen] = useState(false);
  const [name, setName] = useState("Queue");
  const [view, setView] = useState<"queue" | "history">("queue");
  const [plOpen, setPlOpen] = useState(false);
  const [pendingIds, setPendingIds] = useState<string[]>([]);
  const [delOpen, setDelOpen] = useState(false);
  const [delFiles, setDelFiles] = useState(false);
  const [delId, setDelId] = useState<string | null>(null);
  const [delQueueIndex, setDelQueueIndex] = useState<number | null>(null);
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const playlists = useQuery({
    queryKey: ["playlists"],
    queryFn: () => api.get<Playlist[]>("/api/v1/playlists"),
    enabled: plOpen
  });
  const items = (p.queue?.items || []).filter((i): i is PlayerQueueItem => !!i && typeof i.track_id === "string");
  const ids = items.map((i) => i.track_id);
  const { data: tracks } = useQuery({
    queryKey: ["queue-tracks", ids],
    enabled: ids.length > 0 && view === "queue",
    queryFn: async () => {
      const out: Track[] = [];
      for (const id of ids.slice(0, 80)) {
        try {
          out.push(await api.get<Track>(`/api/v1/tracks/${id}`));
        } catch {
          out.push({ id, title: id });
        }
      }
      return out;
    }
  });
  const hist = useQuery({
    queryKey: ["me-history"],
    enabled: view === "history",
    queryFn: () => api.get<ListenTrack[]>("/api/v1/me/history")
  });
  const map = new Map((tracks || []).map((t) => [t.id, t]));
  const current = p.queue?.current_index ?? 0;
  const now = items[current];
  const upcoming = items.slice(current + 1);
  const activeCount = now ? 1 + upcoming.length : upcoming.length;
  const historyTracks = asListenTracks(hist.data);
  const admin = !!me.data?.is_admin;
  const favSet = useMemo(
    () => new Set((favs.data || []).filter((f) => f.type === "track").map((f) => f.id)),
    [favs.data]
  );

  const clearDrag = () => {
    setDragFrom(-1);
    setOverIndex(null);
  };

  const dropAt = (over: number) => {
    const to = queueReorderTarget(dragFrom, over, items.length);
    const from = dragFrom;
    const moved = from >= 0 ? items[from] : undefined;
    clearDrag();
    if (to == null) return;
    p.control("reorder", { from, to });
    if (moved?.id) {
      setFlashId(moved.id);
      window.setTimeout(() => setFlashId((id) => (id === moved.id ? null : id)), 450);
    }
  };

  const removeAt = (i: number) => {
    p.control("remove", { position: i });
  };

  const playNext = (i: number) => {
    if (i === current) return;
    const to = current + 1;
    if (i === to) return;
    p.control("reorder", { from: i, to });
  };

  const toggleFav = async (t: Track) => {
    if (t.source === "youtube" || !isLibraryTrackId(t.id)) {
      toast.message("Play or queue it first so it lands in the library");
      return;
    }
    const on = !favSet.has(t.id);
    await api.post("/api/v1/favourites", { type: "track", id: t.id, on });
    qc.invalidateQueries({ queryKey: ["favourites"] });
    toast.success(on ? "Favourited" : "Removed from favourites");
  };

  const row = (item: PlayerQueueItem, i: number, opts: { nowPlaying?: boolean }) => {
    const t = map.get(item.track_id);
    const addedBy = !opts.nowPlaying ? addedByLabel(item.requested_by, me.data?.id, p.listeners) : null;
    const artistId = t?.artists?.[0]?.id;
    const canDelete = admin && t?.source !== "youtube" && isLibraryTrackId(item.track_id);
    const actions: ({ kind: "item"; label: string; onSelect: () => void; danger?: boolean } | { kind: "sep" })[] = [
      ...(opts.nowPlaying
        ? [{ kind: "item" as const, label: "Restart", onSelect: () => p.seek(0) }]
        : [
            { kind: "item" as const, label: "Play now", onSelect: () => void p.playNow(i) },
            { kind: "item" as const, label: "Play next", onSelect: () => playNext(i) }
          ]),
      { kind: "item", label: "Remove from queue", onSelect: () => removeAt(i) },
      { kind: "sep" },
      ...(t && t.source !== "youtube" && isLibraryTrackId(t.id)
        ? [{ kind: "item" as const, label: favSet.has(t.id) ? "Unfavourite" : "Favourite", onSelect: () => void toggleFav(t) }]
        : []),
      {
        kind: "item",
        label: "Add to playlist",
        onSelect: () => {
          setPendingIds([item.track_id]);
          setPlOpen(true);
        }
      },
      { kind: "item", label: "Save offline", onSelect: () => saveTracksOffline([item.track_id]) },
      ...(t ? [{ kind: "item" as const, label: "Download", onSelect: () => downloadTrack(t) }] : []),
      { kind: "sep" },
      { kind: "item", label: "Go to track info", onSelect: () => navigate(`/tracks/${item.track_id}`) },
      ...(t?.album_id ? [{ kind: "item" as const, label: "Go to album", onSelect: () => navigate(`/albums/${t.album_id}`) }] : []),
      ...(artistId ? [{ kind: "item" as const, label: "Go to artist", onSelect: () => navigate(`/artists/${artistId}`) }] : []),
      ...(isLibraryTrackId(item.track_id)
        ? [{ kind: "item" as const, label: "Start radio", onSelect: () => navigate(`/radio/track/${item.track_id}`) }]
        : []),
      ...(canDelete
        ? [
            { kind: "sep" as const },
            {
              kind: "item" as const,
              label: "Delete from library",
              danger: true,
              onSelect: () => {
                setDelId(item.track_id);
                setDelQueueIndex(i);
                setDelFiles(false);
                setDelOpen(true);
              }
            }
          ]
        : [])
    ];
    const menuNodes = (Item: typeof ContextMenuItem | typeof DropdownMenuItem, Sep: typeof ContextMenuSeparator | typeof DropdownMenuSeparator) =>
      actions.map((a, n) =>
        a.kind === "sep" ? (
          <Sep key={`sep-${n}`} />
        ) : (
          <Item key={a.label} className={a.danger ? "text-destructive" : undefined} onSelect={a.onSelect}>
            {a.label}
          </Item>
        )
      );
    const body = (
      <div
        data-queue-row={item.id}
        onDragOver={(e) => {
          e.preventDefault();
          e.dataTransfer.dropEffect = "move";
          if (overIndex !== i) setOverIndex(i);
        }}
        onDrop={(e) => {
          e.preventDefault();
          dropAt(i);
        }}
        className={cn(
          "group flex items-center gap-2 rounded-md p-2 transition-opacity duration-150",
          opts.nowPlaying ? "bg-surface-2" : "hover:bg-surface-2",
          dragFrom === i && "opacity-40",
          flashId === item.id && "ring-1 ring-accent bg-accent/10"
        )}
      >
        <span
          draggable
          onDragStart={(e) => {
            e.dataTransfer.effectAllowed = "move";
            const rowEl = (e.currentTarget as HTMLElement).closest("[data-queue-row]");
            if (rowEl instanceof HTMLElement) e.dataTransfer.setDragImage(rowEl, 16, 20);
            setDragFrom(i);
            setOverIndex(i);
          }}
          onDragEnd={clearDrag}
          className="flex h-8 w-6 shrink-0 cursor-grab items-center justify-center text-subtle active:cursor-grabbing"
          aria-label="Drag to reorder"
        >
          <GripVertical className="h-3.5 w-3.5" />
        </span>
        <div className="h-10 w-10 overflow-hidden rounded">
          <Artwork src={artworkUrl("track", item.track_id, "thumb")} id={item.track_id} name={t?.title} kind="track" size="sm" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm">{item.title || t?.title || "Track"}</div>
          <div className="truncate text-xs text-muted">
            {item.artist || t?.artists?.map((a) => a.name).join(", ") || t?.artist || (opts.nowPlaying ? "Now playing" : "Up next")}
          </div>
          {addedBy && item.requested_by?.user_id && item.requested_by.user_id !== me.data?.id ? (
            <Link to={`/users/${item.requested_by.user_id}`} className="truncate text-[11px] text-subtle hover:underline">
              {addedBy}
            </Link>
          ) : addedBy ? (
            <div className="truncate text-[11px] text-subtle">{addedBy}</div>
          ) : null}
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8 shrink-0"
              aria-label="Track actions"
              onPointerDown={(e) => e.stopPropagation()}
              onClick={(e) => e.stopPropagation()}
            >
              <MoreHorizontal className="h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
            {menuNodes(DropdownMenuItem, DropdownMenuSeparator)}
          </DropdownMenuContent>
        </DropdownMenu>
        {!opts.nowPlaying && (
          <>
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8 opacity-0 group-hover:opacity-100 focus:opacity-100"
              onClick={(e) => {
                e.stopPropagation();
                p.playNow(i);
              }}
              aria-label="Play now"
            >
              <Play className="h-3.5 w-3.5" />
            </Button>
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8 opacity-0 group-hover:opacity-100 focus:opacity-100"
              onClick={(e) => {
                e.stopPropagation();
                removeAt(i);
              }}
              aria-label="Remove from queue"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </>
        )}
      </div>
    );
    return (
      <div key={item.id}>
        <QueueInsertLine active={showQueueInsertLine(dragFrom, overIndex, i)} />
        <ContextMenu>
          <ContextMenuTrigger asChild>{body}</ContextMenuTrigger>
          <ContextMenuContent>{menuNodes(ContextMenuItem, ContextMenuSeparator)}</ContextMenuContent>
        </ContextMenu>
      </div>
    );
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <h2 className="font-semibold">{view === "history" ? "History" : "Queue"}</h2>
          {view === "queue" && <span className="text-sm text-muted">{activeCount}</span>}
          {showPresence && <QueuePresence className="ml-1" />}
        </div>
        <div className="flex items-center gap-1">
          {view === "queue" ? (
            <Tooltip label="History">
              <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => setView("history")} aria-label="History">
                <History className="h-4 w-4" />
              </Button>
            </Tooltip>
          ) : (
            <Tooltip label="Queue">
              <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => setView("queue")} aria-label="Back to queue">
                <ListMusic className="h-4 w-4" />
              </Button>
            </Tooltip>
          )}
          {onCollapse && (
            <Tooltip label={ui.queuePinned ? "Unpin queue" : "Pin queue open"}>
              <Button
                size="icon"
                variant="ghost"
                className={`h-8 w-8 ${ui.queuePinned ? "text-accent" : ""}`}
                onClick={() => ui.set({ queuePinned: !ui.queuePinned, queueCollapsed: false })}
                aria-label={ui.queuePinned ? "Unpin queue" : "Pin queue open"}
              >
                {ui.queuePinned ? <PinOff className="h-4 w-4" /> : <Pin className="h-4 w-4" />}
              </Button>
            </Tooltip>
          )}
          {onCollapse && (
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onCollapse} aria-label="Close queue">
              <PanelRightClose className="h-4 w-4" />
            </Button>
          )}
          {onClose && (
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onClose} aria-label="Close queue">
              <X className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
      {view === "queue" && (
        <div className="flex justify-end gap-1 px-4">
          <Button
            size="sm"
            variant="ghost"
            disabled={!fillableTrackIds(items).length}
            onClick={() => saveTracksOffline(fillableTrackIds(items))}
          >
            <Download className="mr-1 h-3.5 w-3.5" /> Offline
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setSaveOpen(true)} disabled={!ids.length}>
            <ListPlus className="mr-1 h-3.5 w-3.5" /> Save
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={!upcoming.length}
            onClick={() => p.control("clear")}
          >
            <Trash2 className="mr-1 h-3.5 w-3.5" /> Clear
          </Button>
          {p.pendingUndo && (
            <Button size="sm" variant="ghost" onClick={() => void p.undo()} aria-label="Undo last queue change">
              <Undo2 className="mr-1 h-3.5 w-3.5" /> Undo
            </Button>
          )}
        </div>
      )}
      <div className="min-h-0 flex-1 space-y-3 overflow-auto px-2 py-3 scrollbar-thin">
        {view === "history" ? (
          <>
            <button type="button" className="px-2 text-xs text-muted hover:text-foreground" onClick={() => setView("queue")}>
              ← Back to queue
            </button>
            {!hist.isLoading && !historyTracks.length && (
              <p className="px-2 text-sm text-muted">No listening history yet.</p>
            )}
            {historyTracks.map((t, i) => (
              <button
                key={`${t.id}-${t.played_at}-${i}`}
                type="button"
                className="flex w-full items-center gap-2 rounded-md p-2 text-left hover:bg-surface-2"
                onClick={() => p.playTracks([t.id])}
              >
                <div className="h-10 w-10 overflow-hidden rounded">
                  <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" size="sm" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{t.title}</div>
                  <div className="truncate text-xs text-muted">{t.artist || t.album || "Unknown artist"}</div>
                </div>
                <span className="shrink-0 text-[11px] text-subtle">{relativeTime(t.played_at)}</span>
              </button>
            ))}
          </>
        ) : (
          <>
            {now && (
              <section>
                <div className="mb-1 px-2 text-xs font-semibold uppercase tracking-wide text-subtle">Now playing</div>
                {row(now, current, { nowPlaying: true })}
              </section>
            )}
            <section>
              <div className="mb-1 px-2 text-xs font-semibold uppercase tracking-wide text-subtle">Up next</div>
              {upcoming.map((item, n) => row(item, current + 1 + n, {}))}
              {items.length > 0 && (
                <div
                  className="min-h-3"
                  onDragOver={(e) => {
                    e.preventDefault();
                    if (overIndex !== items.length) setOverIndex(items.length);
                  }}
                  onDrop={(e) => {
                    e.preventDefault();
                    dropAt(items.length);
                  }}
                >
                  <QueueInsertLine active={showQueueInsertLine(dragFrom, overIndex, items.length)} />
                </div>
              )}
              {!upcoming.length && <p className="px-2 text-sm text-muted">{now ? "Nothing queued after this track." : "Queue is empty. Play a track from Home."}</p>}
            </section>
          </>
        )}
      </div>
      <div className="space-y-2 border-t border-border px-4 py-3">
        <label className="flex items-center justify-between gap-3 text-sm" title="Queues library tracks that share genre or tags with the current song. YouTube is only used when those fields exist and the library pool is thin. Title and artist are never searched.">
          <span>
            Autoplay
            <span className="mt-0.5 block text-xs font-normal text-muted">Same genre and tags, then YouTube if needed</span>
          </span>
          <Switch checked={p.autoplay} onCheckedChange={p.setAutoplay} />
        </label>
        <label className="flex items-center justify-between text-sm">
          Stop after current
          <Switch checked={p.stopAfterCurrent} onCheckedChange={p.setStopAfterCurrent} />
        </label>
      </div>
      <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
        <DialogContent title="Save queue as playlist">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await p.saveQueueAsPlaylist(name);
              setSaveOpen(false);
            }}
          >
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} required />
            </Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setSaveOpen(false)}>Cancel</Button>
              <Button type="submit">Save</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={plOpen} onOpenChange={setPlOpen}>
        <DialogContent title="Add to playlist">
          <div className="max-h-72 space-y-1 overflow-auto">
            {(playlists.data || []).map((pl) => (
              <button
                key={pl.id}
                type="button"
                className="block w-full rounded-md px-2 py-2 text-left text-sm hover:bg-surface-2"
                onClick={async () => {
                  await addTracksToPlaylist(pl.id, pendingIds);
                  setPlOpen(false);
                }}
              >
                {pl.name}
              </button>
            ))}
            {!playlists.data?.length && !playlists.isLoading && <p className="text-sm text-muted">No playlists yet.</p>}
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={delOpen} onOpenChange={setDelOpen}>
        <DialogContent title="Remove from library">
          <div className="space-y-3">
            <p className="text-sm text-muted">This removes the track from SoundDock. NAS, local, and external source files are not deleted.</p>
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
                  if (!delId) return;
                  try {
                    await api.post("/api/v1/tracks/bulk", { ids: [delId], delete: true, delete_files: delFiles });
                    removeTracksFromCaches(qc, [delId]);
                    if (delQueueIndex != null) p.control("remove", { position: delQueueIndex });
                    toast.success("Removed");
                    setDelOpen(false);
                    setDelId(null);
                    setDelQueueIndex(null);
                    refreshCatalogue(qc);
                  } catch {
                    toast.error("Could not remove track");
                  }
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
