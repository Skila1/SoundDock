import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { Disc3, Plus } from "lucide-react";
import { api } from "@/lib/api";
import { TrackCard } from "@/components/media/TrackActions";
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
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6 xl:grid-cols-7">
        {Array.from({ length: 14 }).map((_, i) => <Skeleton key={i} className="aspect-square" />)}
      </div>
    );
  }

  if (home.isError) {
    return <QueryError message={home.error instanceof Error ? home.error.message : undefined} onRetry={() => home.refetch()} />;
  }

  if (!mine.length && !recent.length && !added.length && !played.length) {
    return (
      <>
        <EmptyState
          icon={Disc3}
          title="Nothing here yet."
          description="Home lists songs you requested, tracks you played, and the shared catalogue. Start from Search or My Library."
          action={{ label: "Search", onClick: () => nav("/search") }}
        />
        <div className="mt-4 flex justify-center gap-2">
          <Button variant="secondary" onClick={() => nav("/me/library")}>My Library</Button>
        </div>
      </>
    );
  }

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
    <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
      {tracks.map((t, i) => (
        <TrackCard
          key={t.id}
          track={t}
          subtitle={countLabel && t.count ? `${t.count} plays` : t.artist || t.album || "Unknown artist"}
          onPlay={() => onPlay([ids[i]])}
        />
      ))}
    </div>
  );
}
