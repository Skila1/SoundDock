import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, Database, Disc3, HardDrive, Mic2, Music, Server, Users, type LucideIcon } from "lucide-react";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";

function Card({ label, value, icon: Icon, hint }: { label: string; value: ReactNode; icon: LucideIcon; hint?: string }) {
  return (
    <div className="rounded-xl border border-border bg-surface-1 p-4">
      <div className="flex items-center justify-between text-muted">
        <span className="text-sm">{label}</span>
        <Icon className="h-4 w-4" />
      </div>
      <div className="mt-2 text-2xl font-semibold">{value}</div>
      {hint && <div className="mt-1 text-xs text-subtle">{hint}</div>}
    </div>
  );
}

export function AdminOverview() {
  const ov = useQuery({ queryKey: ["admin-overview"], queryFn: () => api.get<any>("/api/v1/admin/overview") });
  const health = useQuery({ queryKey: ["admin-health"], queryFn: () => api.get<any>("/api/v1/admin/health") });
  const db = useQuery({ queryKey: ["admin-db"], queryFn: () => api.get<any>("/api/v1/admin/database") });
  const jobs = useQuery({ queryKey: ["admin-jobs"], queryFn: () => api.get<any[]>("/api/v1/admin/jobs") });
  const discord = useQuery({ queryKey: ["discord"], queryFn: () => api.get<any>("/api/v1/admin/integrations/discord") });
  const c = ov.data?.counts || {};
  const h = health.data || {};
  return (
    <div>
      <PageHeader title="Administration" description="Server health and library status." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={h.postgres ? "success" : "danger"}>{h.postgres ? "Postgres healthy" : "Postgres down"}</Badge>
        <Badge tone={h.ffmpeg ? "success" : "warning"}>{h.ffmpeg ? "FFmpeg" : "FFmpeg missing"}</Badge>
        <Badge tone={h.worker ? "success" : "warning"}>{h.worker ? "Worker ready" : "Draining"}</Badge>
        <Badge tone={discord.data?.token_configured ? "success" : "neutral"}>{discord.data?.token_configured ? "Discord configured" : "Discord idle"}</Badge>
      </div>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Card icon={Music} label="Tracks" value={c.tracks ?? "-"} />
        <Card icon={Disc3} label="Albums" value={c.albums ?? "-"} />
        <Card icon={Mic2} label="Artists" value={c.artists ?? "-"} />
        <Card icon={Users} label="Users" value={c.users ?? "-"} />
        <Card icon={HardDrive} label="Libraries" value={c.libraries ?? "-"} />
        <Card icon={Activity} label="Active streams" value={ov.data?.active_streams ?? 0} />
        <Card icon={Database} label="Database" value={db.data?.database_size || "-"} hint={`migration ${db.data?.migration_version ?? "-"}`} />
        <Card icon={Server} label="Version" value={ov.data?.version || "0.0.1"} />
      </div>
      <p className="mt-4 text-sm text-muted">{(jobs.data || []).filter((j) => j.status === "running" || j.status === "queued").length} active jobs</p>
    </div>
  );
}
