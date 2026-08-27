import { useQuery } from "@tanstack/react-query";
import { Clock } from "lucide-react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { artworkUrl, relativeTime } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import { toast } from "sonner";
import { ListeningNav } from "./ListeningNav";
import { asListenTracks, formatMinutes, sourceLabel, type ListenTrack, type StatsResponse } from "@/features/stats/types";

export function HistoryPage() {
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const hist = useQuery({
    queryKey: ["me-history"],
    queryFn: () => api.get<ListenTrack[]>("/api/v1/me/history")
  });
  const recap = useQuery({
    queryKey: ["me-stats", "week"],
    queryFn: () => api.get<StatsResponse>("/api/v1/me/stats?period=week")
  });
  const tracks = asListenTracks(hist.data);
  const ids = tracks.map((t) => t.id);
  const importedPlays = recap.data?.imported?.plays || 0;

  return (
    <div>
      <PageHeader
        title="Recently played"
        description="Local plays from web and Discord. Imported listens are labelled and stay out of recap totals."
        actions={<LayoutToggle />}
      />
      <ListeningNav />
      <div className="mb-6 grid gap-3 sm:grid-cols-3">
        <RecapCard label="This week" value={`${recap.data?.totals.plays ?? 0} plays`} />
        <RecapCard label="Minutes" value={formatMinutes(recap.data?.totals.minutes)} />
        {importedPlays > 0 && (
          <RecapCard label="Imported this week" value={`${importedPlays} plays`} hint="Not included in recap totals" />
        )}
      </div>
      {!hist.isLoading && !tracks.length && (
        <EmptyState
          icon={Clock}
          title="No listening history yet."
          description="A play counts after 30 seconds or 50% of the track, whichever comes first."
        />
      )}
      <div className="space-y-1">
        {tracks.map((t, i) => (
          <button
            key={`${t.id}-${t.played_at}-${i}`}
            type="button"
            className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left hover:bg-surface-2"
            onClick={() => play([ids[i]])}
            onContextMenu={(e) => {
              e.preventDefault();
              add([t.id], true).then(() => toast.success("Playing next"));
            }}
          >
            <div className="h-10 w-10 shrink-0 overflow-hidden rounded">
              <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" size="sm" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{t.title}</div>
              <div className="truncate text-xs text-muted">{t.artist || t.album || "Unknown artist"}</div>
            </div>
            <div className="hidden shrink-0 items-center gap-2 sm:flex">
              {t.source && (
                <Badge tone={t.source === "import" ? "warning" : "neutral"}>{sourceLabel(t.source)}</Badge>
              )}
              <span className="w-24 text-right text-xs text-subtle">{relativeTime(t.played_at)}</span>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}

function RecapCard({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-xl border border-border bg-surface-1 p-4">
      <p className="text-xs uppercase tracking-wide text-subtle">{label}</p>
      <p className="mt-1 text-lg font-semibold">{value}</p>
      {hint && <p className="mt-1 text-xs text-muted">{hint}</p>}
    </div>
  );
}
