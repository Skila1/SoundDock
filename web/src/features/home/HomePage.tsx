import { useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ChevronLeft, ChevronRight, Disc3, Heart, Plus } from "lucide-react";
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
import type { Track } from "@/types/api";
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

  const favourites = useMemo(() => asTracks(home.data?.favourites).slice(0, 30), [home.data]);
  const mine = useMemo(() => asTracks(home.data?.my_library).slice(0, 15), [home.data]);
  const recent = useMemo(() => asTracks(home.data?.continue).slice(0, 15), [home.data]);
  const added = useMemo(() => asTracks(home.data?.recently_added).slice(0, 15), [home.data]);
  const played = useMemo(() => asTracks(home.data?.most_played).slice(0, 15), [home.data]);

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

  if (!favourites.length && !mine.length && !recent.length && !added.length && !played.length) {
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
  // Favourites and Your library start open; the other shelves start collapsed.
  // Whatever the user toggles is remembered for their account.
  const isOpen = (id: string, fallback: boolean) => view.sections?.[id] ?? fallback;
  const toggle = (id: string, fallback: boolean) => patchView({ sections: { ...view.sections, [id]: !isOpen(id, fallback) } });
  const sections: { id: string; title: string; to?: string; tracks: HomeTrack[]; open: boolean; countLabel?: boolean }[] = [
    { id: "favourites", title: "Favourites", to: "/library/favourites", tracks: favourites, open: true },
    { id: "library", title: "Your library", to: "/me/library", tracks: mine, open: true },
    { id: "recent", title: "Recently played", to: "/history", tracks: recent, open: false },
    { id: "added", title: "Recently added", to: "/library", tracks: added, open: false },
    { id: "most", title: "Most played", to: "/stats", tracks: played, open: false, countLabel: true }
  ];

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
      {sections
        .filter((sec) => sec.tracks.length > 0)
        .map((sec) => {
          const open = isOpen(sec.id, sec.open);
          return (
            <Section key={sec.id} title={sec.title} to={sec.to} count={sec.tracks.length} open={open} onToggle={() => toggle(sec.id, sec.open)}>
              <TrackSection tracks={sec.tracks} layout={layout} onPlay={play} onQueue={add} onNext={playNext} countLabel={sec.countLabel} />
            </Section>
          );
        })}
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

function Section({
  title,
  to,
  count,
  open,
  onToggle,
  children
}: {
  title: string;
  to?: string;
  count: number;
  open: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) {
  const id = `home-${title.toLowerCase().replace(/\s+/g, "-")}`;
  return (
    <section className="border-b border-border/60 pb-6 last:border-0">
      <div className="mb-3 flex items-center justify-between gap-3">
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

