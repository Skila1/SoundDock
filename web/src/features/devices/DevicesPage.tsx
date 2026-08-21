import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Speaker } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { formatDuration } from "@/lib/utils";
import { toast } from "sonner";
import type { Track } from "@/types/api";

type QueueDeviceState = {
  id: string;
  kind?: string;
  owner_key?: string;
  device_id?: string | null;
  volume: number;
  repeat: string;
  shuffle: boolean;
  shuffle_mode?: string;
  stop_after_current?: boolean;
  status: string;
  current_index: number;
  current_track_id?: string | null;
  position_ms: number;
  items: { id: string; position: number; track_id: string }[];
};

const controls = ["resume", "pause", "stop", "previous", "next"] as const;

export function DevicesPage() {
  const qc = useQueryClient();
  const queue = useQuery({
    queryKey: ["me-queue-devices"],
    queryFn: () => api.get<QueueDeviceState>("/api/v1/me/queue"),
    refetchInterval: 5000
  });
  const q = queue.data;
  const track = useQuery({
    queryKey: ["track", q?.current_track_id],
    enabled: !!q?.current_track_id,
    queryFn: () => api.get<Track>(`/api/v1/tracks/${q!.current_track_id}`)
  });

  async function control(action: string) {
    try {
      await api.post("/api/v1/me/queue/control", { action, extra: {} });
      toast.success(action === "resume" ? "Playing here" : action[0].toUpperCase() + action.slice(1));
      qc.invalidateQueries({ queryKey: ["me-queue-devices"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Control failed");
    }
  }

  return (
    <div className="max-w-2xl">
      <PageHeader
        title="Devices"
        description="Handoff playback for your web player. This calls the shared queue API; Discord guild queues are separate."
      />
      {!q && !queue.isLoading && (
        <EmptyState icon={Speaker} title="No playback session" description="Start playing a track to create a web device session." />
      )}
      {q && (
        <article className="space-y-4 rounded-xl border border-border bg-surface-1 p-5">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <Speaker className="h-4 w-4 text-accent" />
                <h2 className="font-semibold">{q.kind === "discord_guild" ? "Discord" : "Web player"}</h2>
                <Badge tone={q.status === "playing" ? "success" : "neutral"}>{q.status}</Badge>
              </div>
              <p className="mt-1 text-sm text-muted">
                {q.device_id ? `Device ${q.device_id}` : "This account’s web session"}
                {q.owner_key ? ` · ${q.owner_key}` : ""}
              </p>
            </div>
          </div>
          <div>
            <div className="text-sm font-medium">{track.data?.title || (q.current_track_id ? q.current_track_id : "Nothing playing")}</div>
            {track.data?.artist && <div className="text-sm text-muted">{track.data.artist}</div>}
            <div className="mt-1 text-xs text-subtle">
              {formatDuration(q.position_ms)} into track · volume {Math.round((q.volume || 0) * 100)}% · repeat {q.repeat}
              {q.shuffle ? " · shuffle" : ""}
              {q.stop_after_current ? " · stop after current" : ""}
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            {controls.map((action) => (
              <Button key={action} size="sm" variant={action === "resume" ? "default" : "outline"} onClick={() => control(action)}>
                {action === "resume" ? "Play here" : action[0].toUpperCase() + action.slice(1)}
              </Button>
            ))}
          </div>
          <div>
            <h3 className="mb-2 text-sm font-semibold">Queue ({q.items?.length || 0})</h3>
            <ul className="max-h-64 space-y-1 overflow-auto text-sm">
              {(q.items || []).map((it) => (
                <li key={it.id} className={it.position === q.current_index ? "text-foreground" : "text-muted"}>
                  {it.position === q.current_index ? "▶ " : ""}
                  {it.position + 1}. {it.track_id}
                </li>
              ))}
            </ul>
          </div>
        </article>
      )}
    </div>
  );
}
