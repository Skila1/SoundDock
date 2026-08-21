import { useQuery } from "@tanstack/react-query";
import { Activity, Database, HardDrive, Server } from "lucide-react";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";

type Health = {
  postgres?: boolean;
  ffmpeg?: boolean;
  ffprobe?: boolean;
  fingerprint?: "available" | "missing" | string;
  worker?: boolean;
  draining?: boolean;
  active_streams?: number;
  version?: string;
  maintenance?: boolean;
  redis_configured?: boolean;
  meilisearch_configured?: boolean;
};

function tone(ok?: boolean) {
  return ok ? "success" : "danger";
}

export function AdminHealth() {
  const q = useQuery({ queryKey: ["admin-health-detail"], queryFn: () => api.get<Health>("/api/v1/admin/health/detail"), refetchInterval: 8000 });
  const h = q.data || {};
  return (
    <div>
      <PageHeader title="Health" description="Service checks. Playback stays up when maintenance is on; /healthz stays 200." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={tone(h.postgres)}>{h.postgres ? "Postgres" : "Postgres down"}</Badge>
        <Badge tone={h.ffmpeg ? "success" : "warning"}>{h.ffmpeg ? "FFmpeg" : "FFmpeg missing"}</Badge>
        <Badge tone={h.ffprobe ? "success" : "warning"}>{h.ffprobe ? "FFprobe" : "FFprobe missing"}</Badge>
        <Badge tone={h.fingerprint === "available" ? "success" : "warning"}>Fingerprint {h.fingerprint || "unknown"}</Badge>
        <Badge tone={h.worker ? "success" : "warning"}>{h.worker ? "Worker ready" : "Draining"}</Badge>
        <Badge tone={h.maintenance ? "warning" : "neutral"}>{h.maintenance ? "Maintenance" : "Live"}</Badge>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex items-center justify-between text-muted">
            <span className="text-sm">Active streams</span>
            <Activity className="h-4 w-4" />
          </div>
          <div className="mt-2 text-2xl font-semibold">{h.active_streams ?? 0}</div>
        </article>
        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex items-center justify-between text-muted">
            <span className="text-sm">Version</span>
            <Server className="h-4 w-4" />
          </div>
          <div className="mt-2 text-2xl font-semibold">{h.version || "—"}</div>
        </article>
        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex items-center justify-between text-muted">
            <span className="text-sm">Redis</span>
            <Database className="h-4 w-4" />
          </div>
          <div className="mt-2 text-lg font-semibold">{h.redis_configured ? "Configured" : "Not configured"}</div>
        </article>
        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex items-center justify-between text-muted">
            <span className="text-sm">Meilisearch</span>
            <HardDrive className="h-4 w-4" />
          </div>
          <div className="mt-2 text-lg font-semibold">{h.meilisearch_configured ? "Configured" : "Not configured"}</div>
        </article>
      </div>
    </div>
  );
}
