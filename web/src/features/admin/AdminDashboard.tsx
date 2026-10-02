import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, ChevronDown, Database, Disc3, HardDrive, Mic2, Music, Server, Users } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/ui/empty";
import { formatBytes, relativeTime } from "@/lib/utils";
import type { StatsRebuildStatus } from "@/types/api";
import { Card, SettingSwitch, StatCard, errorMessage } from "./adminUi";

type CheckStatus = "PASS" | "WARN" | "FAIL" | "SKIP";
type Check = { id: string; name: string; status: CheckStatus; ok?: boolean; detail?: string };

type Health = {
  postgres?: boolean;
  ffmpeg?: boolean;
  ffprobe?: boolean;
  fingerprint?: string;
  worker?: boolean;
  active_streams?: number;
  version?: string;
  maintenance?: boolean;
  redis_configured?: boolean;
  meilisearch_configured?: boolean;
};

type Diag = {
  go?: { version?: string; goroutines?: number; cpus?: number; heap_alloc?: number; sys?: number };
  dirs?: Record<string, { path?: string; bytes?: number; files?: number; ok?: boolean; error?: string }>;
  binaries?: Record<string, boolean | string>;
  failed_jobs?: number;
  last_backup?: { id?: string; status?: string; created_at?: string };
  checks?: Check[];
};

type DbInfo = { database_size?: string; migration_version?: number; dirty?: boolean; tables?: { name: string; size: string }[] };

function checkTone(status: CheckStatus) {
  if (status === "PASS") return "success" as const;
  if (status === "WARN") return "warning" as const;
  if (status === "FAIL") return "danger" as const;
  return "neutral" as const;
}

export function AdminDashboard() {
  const ov = useQuery({ queryKey: ["admin-overview"], queryFn: () => api.get<any>("/api/v1/admin/overview"), refetchInterval: 15000 });
  const health = useQuery({ queryKey: ["admin-health-detail"], queryFn: () => api.get<Health>("/api/v1/admin/health/detail"), refetchInterval: 15000 });
  const db = useQuery({ queryKey: ["admin-db"], queryFn: () => api.get<DbInfo>("/api/v1/admin/database") });
  const updates = useQuery({ queryKey: ["admin-updates"], queryFn: () => api.get<any>("/api/v1/admin/updates") });
  const discord = useQuery({ queryKey: ["discord"], queryFn: () => api.get<any>("/api/v1/admin/integrations/discord") });
  const stats = useQuery({ queryKey: ["admin-stats-rebuild"], queryFn: () => api.get<StatsRebuildStatus>("/api/v1/admin/stats/rebuild") });
  const c = ov.data?.counts || {};
  const h = health.data || {};
  const d = db.data || {};
  const statsPending = stats.data && stats.data.listen_reader !== "events";

  return (
    <div>
      <PageHeader title="Dashboard" description="Server health, library size, and system checks at a glance." />

      <div className="mb-5 flex flex-wrap gap-2">
        <Badge tone={h.postgres ? "success" : "danger"}>{h.postgres ? "Database healthy" : "Database down"}</Badge>
        <Badge tone={h.ffmpeg && h.ffprobe !== false ? "success" : "warning"}>{h.ffmpeg ? (h.ffprobe === false ? "FFprobe missing" : "FFmpeg ready") : "FFmpeg missing"}</Badge>
        <Badge tone={h.worker ? "success" : "warning"}>{h.worker ? "Workers running" : "Workers draining"}</Badge>
        <Badge tone={h.fingerprint === "available" ? "success" : "warning"}>{h.fingerprint === "available" ? "Fingerprinting ready" : "Fingerprinting unavailable"}</Badge>
        <Badge tone={discord.data?.token_configured ? "success" : "neutral"}>{discord.data?.token_configured ? "Discord bot configured" : "Discord bot off"}</Badge>
        {h.maintenance && <Badge tone="warning">Maintenance mode on</Badge>}
        {updates.data?.available && (
          <Link to="/admin/updates"><Badge tone="warning">Update available</Badge></Link>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard icon={Music} label="Tracks" value={c.tracks ?? "-"} />
        <StatCard icon={Disc3} label="Albums" value={c.albums ?? "-"} />
        <StatCard icon={Mic2} label="Artists" value={c.artists ?? "-"} />
        <StatCard icon={Users} label="Users" value={c.users ?? "-"} />
        <StatCard icon={HardDrive} label="Libraries" value={c.libraries ?? "-"} />
        <StatCard icon={Activity} label="Playing now" value={ov.data?.active_streams ?? h.active_streams ?? 0} hint={<Link className="hover:underline" to="/admin/activity">Open Activity</Link>} />
        <StatCard icon={Database} label="Database" value={d.database_size || "-"} hint={`Schema version ${d.migration_version ?? "-"}${d.dirty ? " (needs attention)" : ""}`} tone={d.dirty ? "warning" : undefined} />
        <StatCard icon={Server} label="Version" value={ov.data?.version || h.version || "-"} hint={updates.data?.available ? `${updates.data.latest_version || "Newer"} available` : "Up to date"} />
      </div>

      {statsPending && (
        <div className="mt-5 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-warning/40 bg-warning/10 px-4 py-3 text-sm">
          <span>Listening stats still use the old history table. Run the one-time stats migration to switch over.</span>
          <Button size="sm" variant="secondary" asChild>
            <Link to="/admin/stats-migration">Open stats migration</Link>
          </Button>
        </div>
      )}

      <div className="mt-6 grid gap-4 lg:grid-cols-2">
        <ServerStatusCard />
        <SystemChecksCard health={h} db={d} />
      </div>
    </div>
  );
}

function ServerStatusCard() {
  const qc = useQueryClient();
  const maint = useQuery({ queryKey: ["admin-maintenance"], queryFn: () => api.get<{ maintenance: boolean }>("/api/v1/admin/maintenance") });
  const ann = useQuery({ queryKey: ["admin-announcement"], queryFn: () => api.get<{ announcement: string }>("/api/v1/admin/announcement") });
  const [text, setText] = useState("");
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    if (ann.data) setText(ann.data.announcement || "");
  }, [ann.data]);
  const saved = ann.data?.announcement || "";

  async function setMaintenance(v: boolean) {
    try {
      await api.put("/api/v1/admin/maintenance", { maintenance: v });
      toast.success(v ? "Maintenance mode on" : "Maintenance mode off");
      qc.invalidateQueries({ queryKey: ["admin-maintenance"] });
      qc.invalidateQueries({ queryKey: ["admin-health-detail"] });
    } catch (e) {
      toast.error(errorMessage(e, "Could not change maintenance mode"));
    }
  }

  async function saveAnnouncement(value: string) {
    setSaving(true);
    try {
      await api.put("/api/v1/admin/announcement", { announcement: value });
      toast.success(value ? "Announcement published" : "Announcement cleared");
      qc.invalidateQueries({ queryKey: ["admin-announcement"] });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save announcement"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card title="Server status" description="Pause changes while you work on the server, or show everyone a message.">
      <SettingSwitch
        label="Maintenance mode"
        description="Blocks library, account, settings, and upload changes. Listening, queues, and scrobbles keep working."
        checked={!!maint.data?.maintenance}
        onChange={setMaintenance}
      />
      <form
        className="mt-3 border-t border-border pt-3"
        onSubmit={(e) => {
          e.preventDefault();
          void saveAnnouncement(text.trim());
        }}
      >
        <label className="text-sm font-medium" htmlFor="announcement">Announcement banner</label>
        <p className="mb-2 text-xs text-subtle">Shown at the top of the app for every signed-in user.</p>
        <div className="flex flex-wrap gap-2">
          <Input id="announcement" className="min-w-0 flex-1" value={text} onChange={(e) => setText(e.target.value)} placeholder="No announcement" />
          <Button type="submit" disabled={saving || text.trim() === saved}>Publish</Button>
          {saved && (
            <Button type="button" variant="ghost" disabled={saving} onClick={() => { setText(""); void saveAnnouncement(""); }}>Clear</Button>
          )}
        </div>
      </form>
    </Card>
  );
}

function SystemChecksCard({ health, db }: { health: Health; db: DbInfo }) {
  const [open, setOpen] = useState(false);
  const q = useQuery({ queryKey: ["admin-diagnostics"], queryFn: () => api.get<Diag>("/api/v1/admin/diagnostics") });
  const d = q.data || {};
  const checks = d.checks || [];
  const failed = checks.filter((c) => c.status === "FAIL").length;
  const warned = checks.filter((c) => c.status === "WARN").length;
  return (
    <Card
      title="System checks"
      description="Live probes of the database, workers, disk, backups, and providers."
      actions={<Button size="sm" variant="secondary" disabled={q.isFetching} onClick={() => q.refetch()}>{q.isFetching ? "Checking…" : "Run checks"}</Button>}
    >
      {checks.length > 0 && (
        <div className="mb-3">
          <Badge tone={failed ? "danger" : warned ? "warning" : "success"}>{failed ? `${failed} failed` : warned ? `${warned} warnings` : "All checks passed"}</Badge>
        </div>
      )}
      <ul className="divide-y divide-border rounded-lg border border-border">
        {checks.map((c) => {
          const status = c.status || (c.ok ? "PASS" : "FAIL");
          return (
            <li key={c.id} className="flex items-start justify-between gap-3 px-3 py-2">
              <div className="min-w-0">
                <div className="text-sm font-medium">{c.name}</div>
                {c.detail && <div className="text-xs text-muted">{c.detail}</div>}
              </div>
              <Badge tone={checkTone(status)}>{status}</Badge>
            </li>
          );
        })}
        {!checks.length && <li className="px-3 py-2 text-sm text-muted">{q.isLoading ? "Running checks…" : "No checks reported."}</li>}
      </ul>
      <button type="button" className="mt-3 flex items-center gap-1 text-sm text-muted hover:text-foreground" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <ChevronDown className={`h-4 w-4 transition ${open ? "rotate-180" : ""}`} /> {open ? "Hide technical details" : "Technical details"}
      </button>
      {open && (
        <div className="mt-3 space-y-4 text-sm">
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1">
            <dt className="text-muted">Go runtime</dt><dd>{d.go?.version || "-"} · {d.go?.goroutines ?? "-"} goroutines · {d.go?.cpus ?? "-"} CPUs</dd>
            <dt className="text-muted">Memory</dt><dd>{formatBytes(d.go?.heap_alloc)} heap · {formatBytes(d.go?.sys)} reserved</dd>
            <dt className="text-muted">Failed jobs</dt><dd>{d.failed_jobs ?? 0}</dd>
            <dt className="text-muted">Redis</dt><dd>{health.redis_configured ? "Configured" : "Not configured"}</dd>
            <dt className="text-muted">Meilisearch</dt><dd>{health.meilisearch_configured ? "Configured" : "Not configured"}</dd>
            <dt className="text-muted">Last backup</dt><dd>{d.last_backup?.status ? `${d.last_backup.status}${d.last_backup.created_at ? ` · ${relativeTime(d.last_backup.created_at)}` : ""}` : "None"}</dd>
          </dl>
          {Object.keys(d.binaries || {}).length > 0 && (
            <div className="flex flex-wrap gap-2">
              {Object.entries(d.binaries || {}).map(([k, v]) => (
                <Badge key={k} tone={v === true || v === "available" ? "success" : "warning"}>{k}: {String(v)}</Badge>
              ))}
            </div>
          )}
          {Object.keys(d.dirs || {}).length > 0 && (
            <div>
              <h3 className="mb-1 font-medium">Directories</h3>
              <ul className="space-y-1">
                {Object.entries(d.dirs || {}).map(([k, v]) => (
                  <li key={k} className="flex justify-between gap-3">
                    <span className="min-w-0 truncate"><span className="capitalize">{k}</span> <span className="text-xs text-subtle">{v.path}</span></span>
                    <span className="shrink-0 text-muted">{v.ok ? `${formatBytes(v.bytes)} · ${v.files ?? 0} files` : v.error || "unavailable"}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {(db.tables || []).length > 0 && (
            <div>
              <h3 className="mb-1 font-medium">Largest tables</h3>
              <ul className="max-h-48 space-y-1 overflow-auto scrollbar-thin">
                {(db.tables || []).map((t) => (
                  <li key={t.name} className="flex justify-between gap-3"><span className="truncate font-mono text-xs">{t.name}</span><span className="text-muted">{t.size}</span></li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
