import { useQuery } from "@tanstack/react-query";
import { Music } from "lucide-react";
import { api } from "@/lib/api";
import { TrackList } from "@/components/media/TrackList";
import { MediaCard } from "@/components/media/MediaCard";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState } from "@/components/ui/empty";
import { PageHeader } from "@/components/ui/empty";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import type { Track } from "@/types/api";
import { toast } from "sonner";

export function TracksPage() {
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const layout = useUi((s) => s.libraryLayout);
  const q = useQuery({ queryKey: ["tracks"], queryFn: () => api.get<Track[]>("/api/v1/tracks") });
  const tracks = q.data || [];
  const ids = tracks.map((t) => t.id);
  return (
    <div>
      <PageHeader title="Tracks" description={`${tracks.length} in your libraries`} actions={<LayoutToggle />} />
      {!q.isLoading && !tracks.length && <EmptyState icon={Music} title="No tracks yet." />}
      {layout === "grid" ? (
        <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {tracks.map((t, i) => (
            <MediaCard
              key={t.id}
              className="max-w-none min-w-0 w-full"
              to={`/tracks/${t.id}`}
              id={t.id}
              title={t.title}
              subtitle={t.artist || t.album}
              kind="track"
              explicit={t.explicit}
              onPlay={() => play(ids, i)}
            />
          ))}
        </div>
      ) : (
        <TrackList
          tracks={tracks}
          onPlay={(i) => play(ids, i)}
          onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
          onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
        />
      )}
    </div>
  );
}
