import { useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ChevronLeft, ChevronRight, Disc3, GripVertical, Heart, ListPlus, Loader2, Plus } from "lucide-react";
import { api } from "@/lib/api";
import { TrackCard } from "@/components/media/TrackActions";
import { TrackList } from "@/components/media/TrackList";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState, QueryError } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { usePlayer } from "@/stores/player";
import { useLibraryView } from "@/stores/libraryView";
import { cn } from "@/lib/utils";
import type { Track, TrackPage, User } from "@/types/api";
import { MediaCard } from "@/components/media/MediaCard";
import type { PlaylistListItem } from "@/features/playlists/types";
import { moveSection, orderSections } from "./homeOrder";
import { toast } from "sonner";

type HomeTrack = Track & { count?: number };

function asTracks(rows: any[] | undefined): HomeTrack[] {
  return (rows || []).map((t) => ({
    id: t.id || t.track_id,
    title: t.title,
    artist: t.artist,
    album: t.album,
    album_id: t.album_id,
    duration_ms: t.duration_ms,
    count: t.count
  }));
}

export function HomePage() {
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const nav = useNavigate();
  const [view, patchView] = useLibraryView("home");
  const layout = view.layout;
  const home = useQuery({ queryKey: ["home"], queryFn: () => api.get<any>("/api/v1/home") });

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const playlists = useQuery({ queryKey: ["playlists"], queryFn: () => api.get<PlaylistListItem[]>("/api/v1/playlists") });
  const favourites = useMemo(() => asTracks(home.data?.favourites).slice(0, 30), [home.data]);
  const recent = useMemo(() => asTracks(home.data?.continue).slice(0, 15), [home.data]);
  const myPlaylists = useMemo(() => (playlists.data || []).filter((p) => !p.user_id || p.user_id === me.data?.id), [playlists.data, me.data]);
  // Section being dragged and the section it would land before (null = last).
  const [drag, setDrag] = useState<{ id: string; before: string | null } | null>(null);
  const sectionEls = useRef(new Map<string, HTMLElement>());
  const isOpen = (id: string, fallback: boolean) => view.sections?.[id] ?? fallback;
  // The full library loads only once its section is opened, a page at a time.
  const fullOpen = isOpen("full", false);
  const full = useInfiniteQuery({
    queryKey: ["tracks", { q: "", sort: "" }],
    enabled: fullOpen,
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams({ limit: "100" });
      if (pageParam) p.set("cursor", pageParam);
      return api.get<TrackPage>(`/api/v1/tracks?${p}`);
    },
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined
  });
  const fullTracks = useMemo(() => asTracks(full.data?.pages.flatMap((p) => p.items || [])), [full.data]);

  if (home.isLoading) {
    return (
      <div className="space-y-10">
        <Skeleton className="h-9 w-64" />
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-14" />)}
        </div>
        <div className="flex gap-4 overflow-hidden">
          {Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="aspect-square w-[168px] shrink-0 rounded-xl" />)}
        </div>
      </div>
    );
  }

  if (home.isError) {
    return <QueryError message={home.error instanceof Error ? home.error.message : undefined} onRetry={() => home.refetch()} />;
  }

  const libraryEmpty = !asTracks(home.data?.recently_added).length && !asTracks(home.data?.my_library).length;
  if (!favourites.length && !recent.length && !myPlaylists.length && libraryEmpty) {
    return (
      <EmptyState
        icon={Disc3}
        title="Nothing here yet."
        description="Home lists songs you requested, tracks you played, and the shared catalogue. Search for something to play, or browse the catalogue."
        action={{ label: "Search", onClick: () => nav("/search") }}
        secondaryAction={{ label: "Browse catalogue", onClick: () => nav("/library") }}
      />
    );
  }

  const playNext = (t: HomeTrack) => add([t.id], true).then(() => toast.success("Playing next"));
  // Favourites and Your Playlists start open; the other sections start collapsed.
  // Whatever the user toggles is remembered for their account.
  const toggle = (id: string, fallback: boolean) => patchView({ sections: { ...view.sections, [id]: !isOpen(id, fallback) } });
  const queuePlaylist = async (p: PlaylistListItem) => {
    try {
      const detail = await api.get<{ tracks?: { track_id: string }[] }>(`/api/v1/playlists/${p.id}`);
      const ids = (detail.tracks || []).map((t) => t.track_id).filter(Boolean);
      if (!ids.length) {
        toast.message(`${p.name} has no songs yet.`);
        return;
      }
      await add(ids);
      toast.success(ids.length === 1 ? `Added 1 song from ${p.name}` : `Added ${ids.length} songs from ${p.name}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not queue the playlist");
    }
  };
  const trackSection = (tracks: HomeTrack[]) => <TrackSection tracks={tracks} layout={layout} onPlay={play} onQueue={add} onNext={playNext} />;
  const sections: { id: string; title: string; to?: string; count: React.ReactNode; show: boolean; open: boolean; content: React.ReactNode }[] = [
    { id: "favourites", title: "Favourites", to: "/library/favourites", count: favourites.length, show: favourites.length > 0, open: true, content: trackSection(favourites) },
    {
      id: "playlists",
      title: "Your Playlists",
      to: "/playlists",
      count: myPlaylists.length,
      show: myPlaylists.length > 0,
      open: true,
      content: <PlaylistShelf playlists={myPlaylists} onQueue={queuePlaylist} />
    },
    { id: "recent", title: "Recently played", to: "/history", count: recent.length, show: recent.length > 0, open: false, content: trackSection(recent) },
    {
      id: "full",
      title: "Full Library",
      to: "/library",
      count: fullOpen && full.data ? `${fullTracks.length}${full.hasNextPage ? "+" : ""}` : "",
      show: !libraryEmpty,
      open: false,
      content: (
        <FullLibrary
          tracks={fullTracks}
          layout={layout}
          loading={full.isLoading}
          hasMore={!!full.hasNextPage}
          loadingMore={full.isFetchingNextPage}
          onMore={() => void full.fetchNextPage()}
          onPlay={(i) => play(fullTracks.map((t) => t.id), i)}
          onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
          onNext={playNext}
        />
      )
    }
  ];

  // Sections are dragged by their grip (mouse, touch or pen) and the order is
  // remembered for the account, like the open/closed state.
  const ordered = orderSections(sections, view.order);
  const fullOrder = ordered.map((s) => s.id);
  const visible = ordered.filter((sec) => sec.show);
  const visibleIds = visible.map((s) => s.id);
  const nextVisible = (id: string) => visibleIds[visibleIds.indexOf(id) + 1] ?? null;
  const commitMove = (id: string, before: string | null) => {
    if (before === id || before === nextVisible(id)) return;
    patchView({ order: moveSection(fullOrder, id, before) });
  };
  const dropTarget = (id: string, y: number): string | null => {
    for (const other of visibleIds) {
      if (other === id) continue;
      const el = sectionEls.current.get(other);
      if (!el) continue;
      const r = el.getBoundingClientRect();
      if (y < r.top + r.height / 2) return other;
    }
    return null;
  };
  const gripProps = (id: string) => ({
    onPointerDown: (e: React.PointerEvent<HTMLButtonElement>) => {
      if (e.button !== 0) return;
      e.preventDefault();
      e.currentTarget.setPointerCapture(e.pointerId);
      setDrag({ id, before: nextVisible(id) });
    },
    onPointerMove: (e: React.PointerEvent<HTMLButtonElement>) => {
      if (drag?.id !== id) return;
      const before = dropTarget(id, e.clientY);
      if (before !== drag.before) setDrag({ id, before });
    },
    onPointerUp: () => {
      if (drag?.id === id) commitMove(id, drag.before);
      setDrag(null);
    },
    onPointerCancel: () => setDrag(null),
    onKeyDown: (e: React.KeyboardEvent<HTMLButtonElement>) => {
      const i = visibleIds.indexOf(id);
      if (e.key === "ArrowUp" && i > 0) {
        e.preventDefault();
        commitMove(id, visibleIds[i - 1]);
      } else if (e.key === "ArrowDown" && i < visibleIds.length - 1) {
        e.preventDefault();
        commitMove(id, visibleIds[i + 2] ?? null);
      }
    }
  });
  const moving = !!drag && drag.before !== drag.id && drag.before !== nextVisible(drag.id);

  return (
    <div className="space-y-8">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="text-sm font-medium text-accent">{greeting()}</p>
          <h1 className="mt-1 text-[1.75rem] font-bold leading-tight tracking-tight md:text-[2rem]">Welcome back</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => nav("/playlists")}>
            <Plus className="h-4 w-4" /> Create playlist
          </Button>
          <LayoutToggle value={layout} onChange={(l) => patchView({ layout: l })} />
        </div>
      </div>
      <div className={cn("space-y-6", drag && "select-none")}>
        {visible.map((sec) => {
          const open = isOpen(sec.id, sec.open);
          return (
            <div key={sec.id}>
              {moving && drag?.before === sec.id && <DropLine />}
              <Section
                title={sec.title}
                to={sec.to}
                count={sec.count}
                open={open}
                onToggle={() => toggle(sec.id, sec.open)}
                dragging={drag?.id === sec.id}
                sectionRef={(el) => {
                  if (el) sectionEls.current.set(sec.id, el);
                  else sectionEls.current.delete(sec.id);
                }}
                grip={
                  <button
                    type="button"
                    {...gripProps(sec.id)}
                    aria-label={`Move ${sec.title}. Drag, or use the up and down arrow keys.`}
                    title="Drag to reorder"
                    className={cn(
                      "flex h-8 w-6 shrink-0 touch-none items-center justify-center rounded-md text-subtle transition-colors hover:bg-surface-2 hover:text-foreground focus-visible:text-foreground",
                      drag?.id === sec.id ? "cursor-grabbing" : "cursor-grab"
                    )}
                  >
                    <GripVertical className="h-4 w-4" />
                  </button>
                }
              >
                {sec.content}
              </Section>
            </div>
          );
        })}
        {moving && drag?.before === null && <DropLine />}
      </div>
      {!favourites.length && (
        <p className="rounded-2xl border border-dashed border-border px-4 py-3 text-sm text-muted">
          <Heart className="mr-1.5 inline h-4 w-4 align-[-3px] text-accent" />
          Right-click any song and choose Favourite to pin it to the top of Home.
        </p>
      )}
    </div>
  );
}

function greeting() {
  const h = new Date().getHours();
  return h < 5 ? "Late night listening" : h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}

function DropLine() {
  return <div aria-hidden className="-mt-1 mb-2 h-0.5 rounded-full bg-accent" />;
}

function Section({
  title,
  to,
  count,
  open,
  onToggle,
  grip,
  dragging,
  sectionRef,
  children
}: {
  title: string;
  to?: string;
  count: React.ReactNode;
  open: boolean;
  onToggle: () => void;
  grip?: React.ReactNode;
  dragging?: boolean;
  sectionRef?: (el: HTMLElement | null) => void;
  children: React.ReactNode;
}) {
  const id = `home-${title.toLowerCase().replace(/\s+/g, "-")}`;
  return (
    <section ref={sectionRef} className={cn("border-b border-border/60 pb-6 transition-opacity", dragging && "opacity-50")}>
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-1">
        {grip}
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          aria-controls={id}
          className="group flex min-w-0 items-center gap-2 rounded-lg py-1 text-left"
        >
          <ChevronRight className={cn("h-5 w-5 shrink-0 text-subtle transition-transform duration-200 group-hover:text-foreground", open && "rotate-90")} />
          <h2 className="text-xl font-bold tracking-tight">{title}</h2>
          <span className="tabular text-sm text-subtle">{count}</span>
        </button>
        </div>
        {to && open && (
          <Link to={to} className="text-xs font-semibold uppercase tracking-wider text-subtle transition-colors hover:text-foreground">
            See all
          </Link>
        )}
      </div>
      {open && <div id={id}>{children}</div>}
    </section>
  );
}

function TrackSection({
  tracks,
  layout,
  onPlay,
  onQueue,
  onNext,
  countLabel
}: {
  tracks: HomeTrack[];
  layout: "grid" | "list";
  onPlay: (ids: string[], i?: number) => void;
  onQueue: (ids: string[]) => Promise<void>;
  onNext: (t: HomeTrack) => void;
  countLabel?: boolean;
}) {
  const ids = tracks.map((t) => t.id);
  if (layout === "list") {
    return (
      <TrackList
        tracks={tracks}
        onPlay={(i) => onPlay([ids[i]])}
        onQueue={(t) => onQueue([t.id]).then(() => toast.success("Added to queue"))}
        onNext={(t) => onNext(t)}
      />
    );
  }
  return (
    <Shelf>
      {tracks.map((t, i) => (
        <div key={t.id} className="w-[148px] shrink-0 snap-start sm:w-[168px]">
          <TrackCard
            track={t}
            subtitle={countLabel && t.count ? `${t.count} plays` : t.artist || t.album || "Unknown artist"}
            onPlay={() => onPlay([ids[i]])}
          />
        </div>
      ))}
    </Shelf>
  );
}


/** Your playlists as album-style covers, each with Add to queue. Ignores the grid/list toggle. */
function PlaylistShelf({ playlists, onQueue }: { playlists: PlaylistListItem[]; onQueue: (p: PlaylistListItem) => Promise<void> }) {
  const [busy, setBusy] = useState("");
  return (
    <Shelf>
      {playlists.map((p) => (
        <div key={p.id} className="flex w-[148px] shrink-0 snap-start flex-col gap-2 sm:w-[168px]">
          <MediaCard className="max-w-none min-w-0" to={`/playlists/${p.id}`} id={p.id} title={p.name} kind="playlist" />
          <Button
            size="sm"
            variant="secondary"
            className="w-full"
            disabled={busy === p.id}
            onClick={async () => {
              setBusy(p.id);
              await onQueue(p);
              setBusy("");
            }}
          >
            {busy === p.id ? <Loader2 className="animate-spin" /> : <ListPlus />} Add to queue
          </Button>
        </div>
      ))}
    </Shelf>
  );
}

/** Every song in the catalogue, loaded a page at a time. */
function FullLibrary({
  tracks,
  layout,
  loading,
  hasMore,
  loadingMore,
  onMore,
  onPlay,
  onQueue,
  onNext
}: {
  tracks: HomeTrack[];
  layout: "grid" | "list";
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
  onPlay: (i: number) => void;
  onQueue: (t: HomeTrack) => void;
  onNext: (t: HomeTrack) => void;
}) {
  if (loading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
      </div>
    );
  }
  if (!tracks.length) return <p className="text-sm text-muted">No songs in the library yet.</p>;
  return (
    <div className="space-y-4">
      {layout === "list" ? (
        <TrackList tracks={tracks} onPlay={onPlay} onQueue={onQueue} onNext={onNext} />
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {tracks.map((t, i) => (
            <TrackCard key={t.id} track={t} subtitle={t.artist || t.album || "Unknown artist"} onPlay={() => onPlay(i)} />
          ))}
        </div>
      )}
      {hasMore && (
        <div className="flex justify-center">
          <Button variant="secondary" size="sm" onClick={onMore} disabled={loadingMore}>
            {loadingMore && <Loader2 className="animate-spin" />} Load more songs
          </Button>
        </div>
      )}
    </div>
  );
}

/** Horizontal, snap-scrolling row with arrow buttons on hover. */
function Shelf({ children }: { children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const scroll = (dir: number) => ref.current?.scrollBy({ left: dir * (ref.current.clientWidth * 0.8), behavior: "smooth" });
  return (
    <div className="group/shelf relative -mx-1">
      <div ref={ref} className="flex snap-x snap-mandatory gap-4 overflow-x-auto scroll-px-1 px-1 pb-2">
        {children}
      </div>
      <button
        type="button"
        aria-label="Scroll left"
        onClick={() => scroll(-1)}
        className="absolute -left-3 top-[38%] hidden h-9 w-9 -translate-y-1/2 items-center justify-center rounded-full bg-surface-1 text-foreground opacity-0 shadow-card ring-1 ring-border transition group-hover/shelf:opacity-100 md:flex"
      >
        <ChevronLeft className="h-4 w-4" />
      </button>
      <button
        type="button"
        aria-label="Scroll right"
        onClick={() => scroll(1)}
        className="absolute -right-3 top-[38%] hidden h-9 w-9 -translate-y-1/2 items-center justify-center rounded-full bg-surface-1 text-foreground opacity-0 shadow-card ring-1 ring-border transition group-hover/shelf:opacity-100 md:flex"
      >
        <ChevronRight className="h-4 w-4" />
      </button>
    </div>
  );
}

