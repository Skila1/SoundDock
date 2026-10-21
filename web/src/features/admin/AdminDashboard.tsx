import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ArchiveRestore, CheckCircle2, HardDrive, Headphones, Music, XCircle } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/misc";
import { Switch } from "@/components/ui/switch";
import { cn, formatBytes, relativeTime } from "@/lib/utils";
import type { LyricsGeneratorStatus, StatsRebuildStatus } from "@/types/api";
import { Card, errorMessage } from "./adminUi";
import {
  attentionItems,
  backupTone,
  fixLinks,
  isYouTubeBlock,
  recentErrors,
  storageUsed,
  type Attention,
  type ErrorItem,
  type PillTone
} from "./overview/overviewModel";
import { StatusPill, Tile } from "./overview/overviewUi";

type Overview = { counts?: Record<string, number>; active_streams?: number };
type Health = { postgres?: boolean; ffmpeg?: boolean; ffprobe?: boolean; worker?: boolean; active_streams?: number };
type Discord = { token_configured?: boolean; enabled?: boolean; gateway_status?: string | null; last_error?: string };
type Updates = { available?: boolean; latest_version?: string; current_version?: string };
type StorageRow = { id: string; used_bytes?: number; total_bytes?: number };
type BackupRow = { id: string; status?: string; created_at?: string; destination?: string };
type BackupSettings = { restore_passphrase_set?: boolean };
type Workers = { pools?: { live?: { failed?: number } }[] };

const n = (v?: number) => (v == null ? "-" : v.toLocaleString());

export function AdminDashboard() {
  const ov = useQuery({ queryKey: ["admin-overview"], queryFn: () => api.get<Overview>("/api/v1/admin/overview"), refetchInterval: 15000 });
  const health = useQuery({ queryKey: ["admin-health-detail"], queryFn: () => api.get<Health>("/api/v1/admin/health/detail"), refetchInterval: 15000 });
  const updates = useQuery({ queryKey: ["admin-updates"], queryFn: () => api.get<Updates>("/api/v1/admin/updates") });
  const discord = useQuery({ queryKey: ["discord"], queryFn: () => api.get<Discord>("/api/v1/admin/integrations/discord") });
  const lyrics = useQuery({ queryKey: ["admin-lyrics-generator"], queryFn: () => api.get<LyricsGeneratorStatus>("/api/v1/admin/lyrics/generator") });
  const storage = useQuery({ queryKey: ["admin-storage"], queryFn: () => api.get<StorageRow[]>("/api/v1/admin/storage") });
  const backups = useQuery({ queryKey: ["admin-backups"], queryFn: () => api.get<BackupRow[]>("/api/v1/admin/backups") });
  const backupSettings = useQuery({ queryKey: ["admin-backup-settings"], queryFn: () => api.get<BackupSettings>("/api/v1/admin/backups/settings") });
  const stats = useQuery({ queryKey: ["admin-stats-rebuild"], queryFn: () => api.get<StatsRebuildStatus>("/api/v1/admin/stats/rebuild") });
  const workers = useQuery({ queryKey: ["admin-workers"], queryFn: () => api.get<Workers>("/api/v1/admin/workers"), refetchInterval: 15000 });
  const downloadErrors = useQuery({
    queryKey: ["admin-errors", "acquisition", ""],
    queryFn: () => api.get<{ items?: ErrorItem[] }>("/api/v1/admin/errors?source=acquisition&limit=100"),
    refetchInterval: 60000
  });

  const c = ov.data?.counts || {};
  const h = health.data;
  const d = discord.data;
  const lastBackup = backups.data?.[0];
  const used = storageUsed(storage.data || []);
  const recentDownloads = recentErrors(downloadErrors.data?.items || []).filter((e) => e.status !== "cancelled");

  const attention = attentionItems({
    failedJobs24h: (workers.data?.pools || []).reduce((sum, p) => sum + (p.live?.failed || 0), 0),
    failedDownloads24h: recentDownloads.length,
    youtubeBlocked24h: recentDownloads.filter(isYouTubeBlock).length,
    passphraseMissing: !!backupSettings.data && !backupSettings.data.restore_passphrase_set,
    statsCutoverPending: !!stats.data && stats.data.listen_reader !== "events"
  });
  const attentionLoading = workers.isLoading || downloadErrors.isLoading || backupSettings.isLoading || stats.isLoading;

  return (
    <div>
      <PageHeader title="Overview" description="Is everything healthy, and is there anything you need to do?" />

      <div className="mb-6 flex flex-wrap gap-2">
        {h ? (
          <>
            <StatusPill tone={h.postgres ? "ok" : "fail"} label={h.postgres ? "Database" : "Database down"} to={fixLinks.diagnostics} />
            <StatusPill
              tone={h.ffmpeg && h.ffprobe !== false ? "ok" : "fail"}
              label={!h.ffmpeg ? "FFmpeg missing" : h.ffprobe === false ? "FFprobe missing" : "FFmpeg"}
              to={fixLinks.diagnostics}
            />
            <StatusPill tone={h.worker ? "ok" : "warn"} label={h.worker ? "Workers" : "Workers paused"} to={fixLinks.activity} />
          </>
        ) : (
          <Skeleton className="h-8 w-64 rounded-full" />
        )}
        {d?.token_configured && (
          <StatusPill tone={discordTone(d)} label={discordTone(d) === "ok" ? "Discord bot" : "Discord bot offline"} title={d.last_error} to={fixLinks.discordBot} />
        )}
        {lyrics.data?.configured && (
          <StatusPill
            tone={lyrics.data.status === "ok" ? "ok" : lyrics.data.status === "loading" ? "warn" : "fail"}
            label={lyrics.data.status === "ok" ? "Lyrics generator" : lyrics.data.status === "loading" ? "Lyrics generator starting" : "Lyrics generator unreachable"}
            title={lyrics.data.error}
            to={fixLinks.librarySettings}
          />
        )}
        {updates.data && (
          <StatusPill
            tone={updates.data.available ? "warn" : "ok"}
            label={updates.data.available ? `Update available${updates.data.latest_version ? `: ${updates.data.latest_version}` : ""}` : "Up to date"}
            to={fixLinks.updates}
          />
        )}
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Tile icon={Music} label="Library" value={n(c.tracks)} hint={`tracks · ${n(c.albums)} albums · ${n(c.artists)} artists`} to={fixLinks.library} />
        <Tile icon={Headphones} label="Listening now" value={n(ov.data?.active_streams ?? h?.active_streams)} hint="Open Activity" to={fixLinks.activity} />
        <Tile
          icon={HardDrive}
          label="Storage used"
          value={used ? formatBytes(used.used) : "-"}
          hint={used?.total ? `of ${formatBytes(used.total)}` : "Manage storage"}
          to={fixLinks.storage}
        />
        <Tile
          icon={ArchiveRestore}
          label="Last backup"
          value={lastBackup?.created_at ? relativeTime(lastBackup.created_at) : backups.isLoading ? "-" : "Never"}
          hint={lastBackup ? [lastBackup.status, lastBackup.destination].filter(Boolean).join(" · ") || "Open backups" : "Back up now"}
          tone={backups.isLoading ? undefined : backupTone(lastBackup) === "ok" ? undefined : backupTone(lastBackup)}
          to={fixLinks.backups}
        />
      </div>

      <div className="mt-6 grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <NeedsAttention items={attention} loading={attentionLoading} />
        <ServerNoticeCard />
      </div>
    </div>
  );
}

function discordTone(d: Discord): PillTone {
  if (d.gateway_status === "connected") return "ok";
  if (d.gateway_status === "error" || d.last_error) return "fail";
  return "warn";
}

function NeedsAttention({ items, loading }: { items: Attention[]; loading: boolean }) {
  return (
    <Card title="Needs attention">
      {loading && !items.length ? (
        <div className="space-y-2">
          <Skeleton className="h-14" />
          <Skeleton className="h-14" />
        </div>
      ) : !items.length ? (
        <div className="flex items-center gap-3 rounded-xl bg-success/10 px-4 py-4 text-sm">
          <CheckCircle2 className="h-5 w-5 shrink-0 text-success" />
          <span>All clear. Nothing needs you right now.</span>
        </div>
      ) : (
        <ul className="divide-y divide-border">
          {items.map((a) => {
            const Icon = a.tone === "fail" ? XCircle : AlertTriangle;
            return (
              <li key={a.id} className="flex flex-wrap items-center gap-3 py-3 first:pt-0 last:pb-0">
                <Icon className={cn("h-5 w-5 shrink-0", a.tone === "fail" ? "text-destructive" : "text-warning")} />
                <div className="min-w-0 flex-1 basis-48">
                  <div className="text-sm font-medium">{a.title}</div>
                  <p className="text-xs text-subtle">{a.detail}</p>
                </div>
                <Button size="sm" variant="secondary" asChild>
                  <Link to={a.to}>{a.action}</Link>
                </Button>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}

/** Maintenance mode and the announcement banner: the two "tell everyone" controls. */
function ServerNoticeCard() {
  const qc = useQueryClient();
  const maint = useQuery({ queryKey: ["admin-maintenance"], queryFn: () => api.get<{ maintenance: boolean }>("/api/v1/admin/maintenance") });
  const ann = useQuery({ queryKey: ["admin-announcement"], queryFn: () => api.get<{ announcement: string }>("/api/v1/admin/announcement") });
  const [text, setText] = useState("");
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    if (ann.data) setText(ann.data.announcement || "");
  }, [ann.data]);
  const saved = ann.data?.announcement || "";
  const on = !!maint.data?.maintenance;

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
    <Card title="Tell everyone" className={cn(on && "border-warning/50")}>
      <div className="flex items-center justify-between gap-4">
        <div className="min-w-0">
          <div className="text-sm font-medium">Maintenance mode{on && <span className="ml-2 text-xs font-semibold text-warning">On</span>}</div>
          <p className="text-xs text-subtle">Pauses changes. Listening keeps working.</p>
        </div>
        <Switch checked={on} onCheckedChange={(v) => void setMaintenance(v)} aria-label="Maintenance mode" />
      </div>
      <form
        className="mt-4 border-t border-border pt-4"
        onSubmit={(e) => {
          e.preventDefault();
          void saveAnnouncement(text.trim());
        }}
      >
        <label className="text-sm font-medium" htmlFor="announcement">Announcement</label>
        <p className="mb-2 text-xs text-subtle">A banner at the top of the app for everyone.</p>
        <div className="flex flex-wrap gap-2">
          <Input id="announcement" className="min-w-0 flex-1 basis-40" value={text} onChange={(e) => setText(e.target.value)} placeholder="No announcement" />
          <div className="flex gap-2">
            <Button type="submit" size="sm" className="h-10" disabled={saving || text.trim() === saved}>Publish</Button>
            {saved && (
              <Button type="button" size="sm" variant="ghost" className="h-10" disabled={saving} onClick={() => { setText(""); void saveAnnouncement(""); }}>Clear</Button>
            )}
          </div>
        </div>
      </form>
    </Card>
  );
}
