import { useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ChevronLeft, ChevronRight, Disc3, Play, Plus } from "lucide-react";
import { api } from "@/lib/api";
import { TrackCard } from "@/components/media/TrackActions";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl } from "@/lib/utils";
import { TrackList } from "@/components/media/TrackList";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState, QueryError } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
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
  const layout = useUi((s) => s.libraryLayout);
  const home = useQuery({ queryKey: ["home"], queryFn: () => api.get<any>("/api/v1/home") });

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

  if (!mine.length && !recent.length && !added.length && !played.length) {
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

  const quick = uniqueTracks([...recent, ...mine]).slice(0, 6);
  const playNext = (t: HomeTrack) => add([t.id], true).then(() => toast.success("Playing next"));

  return (
    <div className="space-y-12">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="text-sm font-medium text-accent">{greeting()}</p>
          <h1 className="mt-1 text-[1.75rem] font-bold leading-tight tracking-tight md:text-[2rem]">Welcome back</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => nav("/playlists")}>
            <Plus className="h-4 w-4" /> Create playlist
          </Button>
          <LayoutToggle />
        </div>
      </div>
      {quick.length >= 2 && <QuickPicks tracks={quick} onPlay={(id) => play([id])} />}
      {!!mine.length && (
        <Section title="Your library" to="/me/library">
          <TrackSection tracks={mine} layout={layout} onPlay={play} onQueue={add} onNext={playNext} />
        </Section>
      )}
      {!!recent.length && (
        <Section title="Recently played" to="/history">
          <TrackSection tracks={recent} layout={layout} onPlay={play} onQueue={add} onNext={playNext} />
        </Section>
      )}
      {!!added.length && (
        <Section title="Recently added" to="/library">
          <TrackSection tracks={added} layout={layout} onPlay={play} onQueue={add} onNext={playNext} />
        </Section>
      )}
      {!!played.length && (
        <Section title="Most played" to="/stats">
          <TrackSection tracks={played} layout={layout} onPlay={play} onQueue={add} onNext={playNext} countLabel />
        </Section>
      )}
    </div>
  );
}

function greeting() {
  const h = new Date().getHours();
  return h < 5 ? "Late night listening" : h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}

function Section({ title, to, children }: { title: string; to?: string; children: React.ReactNode }) {
  return (
    <section>
      <div className="mb-4 flex items-baseline justify-between gap-3">
        <h2 className="text-xl font-bold tracking-tight">{title}</h2>
        {to && (
          <Link to={to} className="text-xs font-semibold uppercase tracking-wider text-subtle transition-colors hover:text-foreground">
            See all
          </Link>
        )}
      </div>
      {children}
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

function uniqueTracks(list: HomeTrack[]) {
  const seen = new Set<string>();
  return list.filter((t) => (seen.has(t.id) ? false : (seen.add(t.id), true)));
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

/** Six quick tiles to resume recent listening in one click. */
function QuickPicks({ tracks, onPlay }: { tracks: HomeTrack[]; onPlay: (id: string) => void }) {
  return (
    <section className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
      {tracks.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => onPlay(t.id)}
          className="group flex items-center gap-3 overflow-hidden rounded-xl bg-surface-2/60 pr-3 text-left ring-1 ring-inset ring-border transition hover:bg-surface-2"
        >
          <div className="h-14 w-14 shrink-0 overflow-hidden">
            <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{t.title}</div>
            <div className="truncate text-xs text-muted">{t.artist || t.album}</div>
          </div>
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent text-[#04140a] opacity-0 shadow-card transition group-hover:opacity-100">
            <Play className="ml-0.5 h-4 w-4 fill-current" />
          </span>
        </button>
      ))}
    </section>
  );
}
