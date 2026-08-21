import { useQuery } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { api } from "@/lib/api";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { relativeTime } from "@/lib/utils";
import { ListeningNav } from "./ListeningNav";
import { ListenTrackList } from "./ListenTrackList";
import { asListenTracks, type ListenTrack } from "@/features/stats/types";

export function RediscoveryPage() {
  const q = useQuery({
    queryKey: ["me-rediscovery"],
    queryFn: () => api.get<ListenTrack[]>("/api/v1/me/rediscovery?days=60")
  });
  const tracks = asListenTracks(q.data);

  return (
    <div>
      <PageHeader
        title="Rediscovery"
        description="Tracks you used to play that have been quiet for at least 60 days."
        actions={<LayoutToggle />}
      />
      <ListeningNav />
      {!q.isLoading && !tracks.length && (
        <EmptyState
          icon={RotateCcw}
          title="Nothing to rediscover yet."
          description="After a few weeks away from old favourites, they will show up here."
        />
      )}
      {tracks.length > 0 && (
        <ListenTrackList
          tracks={tracks}
          subtitle={(t) => {
            const last = t.last_played_at ? relativeTime(t.last_played_at) : "a while ago";
            const plays = t.count || t.plays || 0;
            return `${t.artist || t.album || "Unknown artist"} · ${plays} plays · ${last}`;
          }}
        />
      )}
    </div>
  );
}
