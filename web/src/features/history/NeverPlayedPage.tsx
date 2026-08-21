import { useQuery } from "@tanstack/react-query";
import { Disc3 } from "lucide-react";
import { api } from "@/lib/api";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { ListeningNav } from "./ListeningNav";
import { ListenTrackList } from "./ListenTrackList";
import { asListenTracks, type ListenTrack } from "@/features/stats/types";

export function NeverPlayedPage() {
  const q = useQuery({
    queryKey: ["me-never-played"],
    queryFn: () => api.get<ListenTrack[]>("/api/v1/me/never-played")
  });
  const tracks = asListenTracks(q.data);

  return (
    <div>
      <PageHeader
        title="Never played"
        description="Tracks in your libraries that have not crossed the play threshold yet."
        actions={<LayoutToggle />}
      />
      <ListeningNav />
      {!q.isLoading && !tracks.length && (
        <EmptyState icon={Disc3} title="You've played everything in reach." description="New uploads will land here until you play them." />
      )}
      {tracks.length > 0 && <ListenTrackList tracks={tracks} />}
    </div>
  );
}
