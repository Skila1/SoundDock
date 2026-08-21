import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { Disc3, Plus } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { TrackList } from "@/components/media/TrackList";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState } from "@/components/ui/empty";
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
    duration_ms: t.duration_ms
  }));
}

export function HomePage() {
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const nav = useNavigate();
  const layout = useUi((s) => s.libraryLayout);
  const home = useQuery({ queryKey: ["home"], queryFn: () => api.get<any>("/api/v1/home") });

  const recent = useMemo(() => asTracks(home.data?.continue?.length ? home.data.continue : home.data?.recently_added), [home.data]);
  const added = useMemo(() => asTracks(home.data?.recently_added), [home.data]);
  const played = useMemo(() => asTracks(home.data?.most_played), [home.data]);

  if (home.isLoading) {
    return (
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6 xl:grid-cols-7">
        {Array.from({ length: 14 }).map((_, i) => <Skeleton key={i} className="aspect-square" />)}
      </div>
    );
  }

  if (!recent.length && !added.length) {
    return (
      <EmptyState
        icon={Disc3}
        title="Your library is empty."
        description="Upload music or import a file you host. Sign-in is Discord-only."
        action={{ label: "Upload music", onClick: () => nav("/upload") }}
      />
    );
  }

  const heading = home.data?.continue?.length ? "Recently played" : "Recently added";
  const playNext = (t: HomeTrack) => add([t.id], true).then(() => toast.success("Playing next"));

  return (
    <div>
      <div className="mb-6 flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-baseline gap-3">
          <h1 className="text-3xl font-semibold">{heading}</h1>
          {home.data?.continue?.length > 0 && (
            <Link to="/history" className="text-sm text-muted hover:underline">See all</Link>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => nav("/playlists")}>
            <Plus className="h-4 w-4" /> Create playlist
          </Button>
          <LayoutToggle />
        </div>
      </div>
      <TrackSection tracks={recent} layout={layout} onPlay={play} onQueue={add} onNext={playNext} />
      {played.length > 0 && (
        <>
          <div className="mb-4 mt-10 flex items-center justify-between">
            <h2 className="text-xl font-semibold">Most played</h2>
            <Link to="/stats" className="text-sm text-muted hover:underline">See all</Link>
          </div>
          <TrackSection tracks={played} layout={layout} onPlay={play} onQueue={add} onNext={playNext} />
        </>
      )}
      {added.length > 0 && home.data?.continue?.length > 0 && (
        <>
          <div className="mb-4 mt-10 flex items-center justify-between">
            <h2 className="text-xl font-semibold">Recently added</h2>
          </div>
          <TrackSection tracks={added} layout={layout} onPlay={play} onQueue={add} onNext={playNext} />
        </>
      )}
    </div>
  );
}

function TrackSection({
  tracks,
  layout,
  onPlay,
  onQueue,
  onNext
}: {
  tracks: HomeTrack[];
  layout: "grid" | "list";
  onPlay: (ids: string[], i?: number) => void;
  onQueue: (ids: string[]) => Promise<void>;
  onNext: (t: HomeTrack) => void;
}) {
  const ids = tracks.map((t) => t.id);
  if (layout === "list") {
    return (
      <TrackList
        tracks={tracks}
        onPlay={(i) => onPlay(ids, i)}
        onQueue={(t) => onQueue([t.id]).then(() => toast.success("Added to queue"))}
        onNext={(t) => onNext(t)}
      />
    );
  }
  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
      {tracks.map((t, i) => (
        <MediaCard
          key={t.id}
          className="max-w-none min-w-0 w-full"
          to={t.album_id ? `/albums/${t.album_id}` : "/tracks"}
          id={t.id}
          title={t.title}
          subtitle={t.artist || t.album || "Unknown artist"}
          kind="track"
          onPlay={() => onPlay(ids, i)}
        />
      ))}
    </div>
  );
}
