import { MediaCard } from "@/components/media/MediaCard";
import { TrackList } from "@/components/media/TrackList";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { toast } from "sonner";
import type { ListenTrack } from "@/features/stats/types";

export function ListenTrackList({
  tracks,
  subtitle
}: {
  tracks: ListenTrack[];
  subtitle?: (t: ListenTrack) => string | undefined;
}) {
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const layout = useUi((s) => s.libraryLayout);
  const ids = tracks.map((t) => t.id);

  if (layout === "list") {
    return (
      <TrackList
        tracks={tracks}
        onPlay={(i) => play([ids[i]])}
        onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
        onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
      />
    );
  }

  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
      {tracks.map((t, i) => (
        <MediaCard
          key={`${t.id}-${i}`}
          className="max-w-none min-w-0 w-full"
          to={t.album_id ? `/albums/${t.album_id}` : "/tracks"}
          id={t.id}
          title={t.title}
          subtitle={subtitle?.(t) || t.artist || t.album || "Unknown artist"}
          kind="track"
          onPlay={() => play([ids[i]])}
        />
      ))}
    </div>
  );
}
