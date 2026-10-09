import { useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, ListMusic, ListPlus, Loader2, Play, Plus } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { cn, formatDuration } from "@/lib/utils";
import { usePlayer, type QueueTrackHint } from "@/stores/player";
import { Button } from "@/components/ui/button";
import { Badge, Skeleton } from "@/components/ui/misc";
import { EmptyState, QueryError } from "@/components/ui/empty";
import { TaskProgress, type TaskState } from "@/components/ui/task-progress";
import { LocalSearch } from "@/components/media/LibraryToolbar";
import { MediaHero } from "@/components/media/MediaHero";

type ExtTrack = {
  provider_track_id: string;
  title: string;
  artists?: string[];
  album?: string;
  duration_ms?: number;
  isrc?: string;
  artwork?: string;
  explicit?: boolean;
};

type ExtPlaylist = { id?: string; name?: string; description?: string; owner?: string; artwork?: string; track_count?: number };

type Resolved = { ref?: string; source: "library" | "youtube" | "none"; title?: string; artist?: string; duration_ms?: number };

const BATCH = 25;

/**
 * A streaming-service playlist opened inside SoundDock. Songs play, queue or
 * save straight away: library copies are used when we have them, otherwise the
 * matching song is fetched from YouTube.
 */
export function ProviderPlaylistPage() {
  const { provider = "", id = "" } = useParams();
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const q = useQuery({
    queryKey: ["provider-playlist", provider, id],
    queryFn: () => api.get<{ playlist: ExtPlaylist; tracks: ExtTrack[] }>(`/api/v1/providers/${provider}/playlists/${encodeURIComponent(id)}`),
    staleTime: 5 * 60_000
  });
  const tracks = useMemo(() => q.data?.tracks || [], [q.data]);
  const [resolved, setResolved] = useState<Record<number, Resolved>>({});
  const resolvedRef = useRef<Record<number, Resolved>>({});
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [task, setTask] = useState<TaskState | null>(null);
  const [busyRow, setBusyRow] = useState<number | null>(null);
  const [term, setTerm] = useState("");
  const running = task?.status === "running";

  const visible = useMemo(() => {
    const t = term.trim().toLowerCase();
    const rows = tracks.map((tr, i) => ({ tr, i }));
    if (!t) return rows;
    return rows.filter(({ tr }) => `${tr.title} ${(tr.artists || []).join(" ")} ${tr.album || ""}`.toLowerCase().includes(t));
  }, [tracks, term]);

  /** Resolve the given rows (cached), reporting progress, in playlist order. */
  const resolve = async (indices: number[], label: string, onBatch?: (batch: { refs: string[]; hints: QueueTrackHint[] }) => Promise<void>) => {
    const out: { refs: string[]; hints: QueueTrackHint[] } = { refs: [], hints: [] };
    let done = 0;
    let failed = 0;
    setTask({ label, done: 0, total: indices.length, status: "running" });
    for (let start = 0; start < indices.length; start += BATCH) {
      const chunk = indices.slice(start, start + BATCH);
      const need = chunk.filter((i) => !resolvedRef.current[i]);
      if (need.length) {
        try {
          const r = await api.post<{ items: Resolved[] }>("/api/v1/providers/resolve", {
            tracks: need.map((i) => ({ title: tracks[i].title, artists: tracks[i].artists || [], duration_ms: tracks[i].duration_ms || 0, isrc: tracks[i].isrc || "" }))
          });
          const next = { ...resolvedRef.current };
          need.forEach((i, k) => (next[i] = r.items[k] || { source: "none" }));
          resolvedRef.current = next;
          setResolved(next);
        } catch (err) {
          setTask({ label, done, total: indices.length, failed, status: "error", detail: err instanceof Error ? err.message : "Could not look up songs" });
          throw err;
        }
      }
      const batch: { refs: string[]; hints: QueueTrackHint[] } = { refs: [], hints: [] };
      for (const i of chunk) {
        const res = resolvedRef.current[i];
        if (!res?.ref) {
          failed++;
          continue;
        }
        batch.refs.push(res.ref);
        batch.hints.push({ id: res.ref, title: res.title || tracks[i].title, artist: res.artist || (tracks[i].artists || []).join(", "), duration_ms: res.duration_ms || tracks[i].duration_ms });
      }
      done += chunk.length;
      out.refs.push(...batch.refs);
      out.hints.push(...batch.hints);
      if (onBatch && batch.refs.length) await onBatch(batch);
      setTask({ label, done, total: indices.length, failed, status: "running" });
    }
    setTask({ label, done, total: indices.length, failed, status: "done", detail: `${out.refs.length} ready` });
    return out;
  };

  const targets = () => (selected.size ? [...selected].sort((a, b) => a - b) : tracks.map((_, i) => i));

  const playFrom = async (indices: number[]) => {
    let first = true;
    try {
      await resolve(indices, "Finding songs to play", async (b) => {
        if (first) {
          first = false;
          await play(b.refs, 0, b.hints);
        } else {
          await add(b.refs, false, b.hints);
        }
      });
    } catch {
      /* progress shows the error */
    }
  };

  const queue = async (indices: number[]) => {
    try {
      const r = await resolve(indices, "Adding to queue", (b) => add(b.refs, false, b.hints));
      if (r.refs.length) toast.success(r.refs.length === 1 ? "Added to queue" : `Added ${r.refs.length} songs to queue`);
    } catch {
      /* progress shows the error */
    }
  };

  const save = async (indices: number[]) => {
    let added = 0;
    try {
      await resolve(indices, "Adding to My Library", async (b) => {
        const r = await api.post<{ added: number }>("/api/v1/me/library", { refs: b.refs, tracks: b.hints });
        added += r.added || 0;
      });
      void qc.invalidateQueries({ queryKey: ["personal-library"] });
      toast.success(added === 1 ? "Added 1 song to My Library" : `Added ${added} songs to My Library. New downloads finish in the background.`);
    } catch {
      /* progress shows the error */
    }
  };

  const importPlaylist = async () => {
    try {
      await api.post(`/api/v1/providers/${provider}/playlists/${encodeURIComponent(id)}/import`, { mode: "once", name: q.data?.playlist?.name });
      toast.success("Import started. Track its progress on My Playlists.");
      void qc.invalidateQueries({ queryKey: ["playlists"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Import failed");
    }
  };

  const rowAction = async (i: number, fn: (idx: number[]) => Promise<void>) => {
    setBusyRow(i);
    try {
      await fn([i]);
    } finally {
      setBusyRow(null);
    }
  };

  const toggle = (i: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });

  const pl = q.data?.playlist;
  const scope = selected.size ? `${selected.size} selected` : "all";

  return (
    <div>
      {q.isLoading && (
        <div className="space-y-3">
          <Skeleton className="h-40 w-40" />
          <Skeleton className="h-8 w-64" />
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
        </div>
      )}
      {q.isError && <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />}
      {q.data && (
        <>
          <MediaHero
            art={pl?.artwork ? <img src={pl.artwork} alt="" className="h-full w-full object-cover" /> : <div className="flex h-full w-full items-center justify-center"><ListMusic className="h-12 w-12 text-subtle" /></div>}
            backdrop={pl?.artwork}
            eyebrow={`${provider.replace("_", " ")} playlist`}
            title={pl?.name || "Playlist"}
            meta={pl?.owner ? <span className="font-semibold">{pl.owner}</span> : undefined}
            stats={`${tracks.length} songs${selected.size ? ` · ${selected.size} selected` : ""}`}
            onPlay={() => void playFrom(targets())}
            busy={running}
            playDisabled={!tracks.length}
            actions={
              <>
                <Button variant="secondary" size="sm" onClick={() => void queue(targets())} disabled={running || !tracks.length}>
                  <ListPlus /> Queue {scope}
                </Button>
                <Button variant="secondary" size="sm" onClick={() => void save(targets())} disabled={running || !tracks.length}>
                  <Plus /> Add {scope} to My Library
                </Button>
              </>
            }
            menu={[{ label: "Import as SoundDock playlist", icon: <Download className="h-4 w-4" />, onSelect: () => void importPlaylist() }]}
          />
          <TaskProgress task={task} className="mb-4" onDismiss={() => setTask(null)} />
          {tracks.length > 0 && (
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <LocalSearch value={term} onChange={setTerm} placeholder="Filter songs" className="w-full sm:w-72" />
              <label className="flex items-center gap-2 px-2 text-xs text-subtle">
                <input
                  type="checkbox"
                  className="h-4 w-4 accent-accent"
                  checked={selected.size > 0 && selected.size === tracks.length}
                  onChange={() => setSelected(selected.size === tracks.length ? new Set() : new Set(tracks.map((_, i) => i)))}
                />
                Select all
              </label>
              {selected.size > 0 && (
                <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>
                  Clear selection
                </Button>
              )}
            </div>
          )}
          {!tracks.length && <EmptyState icon={ListMusic} title="This playlist is empty." />}
          <div className="divide-y divide-border/60">
            {visible.map(({ tr, i }) => {
              const res = resolved[i];
              return (
                <div
                  key={`${tr.provider_track_id}-${i}`}
                  className={cn("group grid grid-cols-[20px_28px_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-2 py-1.5 hover:bg-surface-2 md:grid-cols-[20px_28px_minmax(0,1fr)_minmax(0,0.8fr)_56px_auto]", selected.has(i) && "bg-surface-2")}
                >
                  <input type="checkbox" aria-label={`Select ${tr.title}`} className="h-4 w-4 accent-accent" checked={selected.has(i)} onChange={() => toggle(i)} />
                  <button type="button" className="flex h-7 w-7 items-center justify-center text-xs text-subtle hover:text-foreground" onClick={() => void rowAction(i, playFrom)} aria-label={`Play ${tr.title}`}>
                    {busyRow === i ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <span className="group-hover:hidden">{i + 1}</span>}
                    {busyRow !== i && <Play className="hidden h-3.5 w-3.5 fill-current group-hover:block" />}
                  </button>
                  <div className="flex min-w-0 items-center gap-3">
                    {tr.artwork && <img src={tr.artwork} alt="" className="hidden h-10 w-10 shrink-0 rounded object-cover sm:block" loading="lazy" />}
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate text-sm font-medium">{tr.title}</span>
                        {tr.explicit && <Badge tone="warning">E</Badge>}
                        {res?.source === "library" && <Badge tone="success">In library</Badge>}
                        {res?.source === "none" && <Badge>Not found</Badge>}
                      </div>
                      <div className="truncate text-xs text-muted">{(tr.artists || []).join(", ")}</div>
                    </div>
                  </div>
                  <div className="hidden truncate text-sm text-muted md:block">{tr.album}</div>
                  <div className="hidden text-right text-xs text-subtle md:block">{formatDuration(tr.duration_ms)}</div>
                  <div className="flex gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100">
                    <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="Add to queue" title="Add to queue" disabled={running} onClick={() => void rowAction(i, queue)}>
                      <ListPlus className="h-3.5 w-3.5" />
                    </Button>
                    <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="Add to My Library" title="Add to My Library" disabled={running} onClick={() => void rowAction(i, save)}>
                      <Plus className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}
