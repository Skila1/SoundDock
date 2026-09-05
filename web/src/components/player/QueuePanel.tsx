import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Download, GripVertical, History, ListMusic, ListPlus, PanelRightClose, Pin, PinOff, Play, Trash2, Undo2, X } from "lucide-react";
import { fillableTrackIds, saveTracksOffline } from "@/lib/offlineFill";
import { Button } from "@/components/ui/button";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl, cn, relativeTime } from "@/lib/utils";
import { usePlayer, type PlayerQueueItem, type RequestedBy } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Track, User } from "@/types/api";
import { Switch } from "@/components/ui/switch";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu";
import { Tooltip } from "@/components/ui/tooltip";
import { asListenTracks, type ListenTrack } from "@/features/stats/types";
import type { PresenceParticipant, PresenceSource } from "@/stores/sseClient";
import { SoftBoundary } from "@/app/ErrorBoundary";
import { avatarDisplaySrc, pageIsActive } from "./presenceAvatar";

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
  const drag = useRef<number>(-1);
  const [saveOpen, setSaveOpen] = useState(false);
  const [name, setName] = useState("Queue");
  const [view, setView] = useState<"queue" | "history">("queue");
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
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

  const removeAt = (i: number) => {
    if (i === current) return;
    p.control("remove", { position: i });
  };

  const row = (item: PlayerQueueItem, i: number, opts: { nowPlaying?: boolean }) => {
    const t = map.get(item.track_id);
    const addedBy = !opts.nowPlaying ? addedByLabel(item.requested_by, me.data?.id, p.listeners) : null;
    const body = (
      <div
        draggable
        onDragStart={() => {
          drag.current = i;
        }}
        onDragOver={(e) => e.preventDefault()}
        onDrop={() => {
          const from = drag.current;
          if (from < 0 || from === i) return;
          p.control("reorder", { from, to: i }).then(() => p.load());
        }}
        className={`group flex cursor-grab items-center gap-2 rounded-md p-2 active:cursor-grabbing ${opts.nowPlaying ? "bg-surface-2" : "hover:bg-surface-2"}`}
      >
        <GripVertical className="h-3.5 w-3.5 shrink-0 text-subtle" />
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
    if (opts.nowPlaying) return <div key={item.id}>{body}</div>;
    return (
      <ContextMenu key={item.id}>
        <ContextMenuTrigger asChild>{body}</ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onSelect={() => p.playNow(i)}>Play now</ContextMenuItem>
          <ContextMenuItem onSelect={() => removeAt(i)}>Remove from queue</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
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
              {!upcoming.length && <p className="px-2 text-sm text-muted">{now ? "Nothing queued after this track." : "Queue is empty. Play a track from Home."}</p>}
            </section>
          </>
        )}
      </div>
      <div className="space-y-2 border-t border-border px-4 py-3">
        <label className="flex items-center justify-between gap-3 text-sm" title="Queues similar library tracks from recent listening, skipping the current queue and recently played songs. YouTube is only used if the library pool is thin.">
          <span>
            Autoplay
            <span className="mt-0.5 block text-xs font-normal text-muted">Similar library tracks, then YouTube if needed</span>
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
    </div>
  );
}
