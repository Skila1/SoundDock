import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Select } from "@/components/ui/select";
import { SettingSwitch } from "../adminUi";
import { relativeTime } from "@/lib/utils";
import { toast } from "sonner";

type Live = {
  active_workers: number;
  busy: number;
  idle: number;
  queue_depth: number;
  running: number;
  failed: number;
  avg_duration_ms: number;
  oldest_queued_at?: string | null;
  ephemeral: number;
};

type Pool = {
  id: string;
  name: string;
  description: string;
  reserved: boolean;
  job_types: string[];
  enabled: boolean;
  min_workers: number;
  max_workers: number;
  queue_limit: number;
  timeout_seconds: number;
  priority: number;
  max_rss_mb: number;
  live: Live;
};

type JobRow = {
  id: string;
  type: string;
  pool: string;
  status: string;
  progress: number;
  attempts: number;
  last_error?: string | null;
  created_at: string;
  started_at?: string | null;
  cancellable?: boolean;
};

type Workers = { pools: Pool[]; running: JobRow[]; jobs: JobRow[] };

function jobTone(status: string) {
  if (status === "failed") return "danger" as const;
  if (status === "completed") return "success" as const;
  if (status === "cancelled") return "warning" as const;
  if (status === "running") return "accent" as const;
  return "neutral" as const;
}

function formatMs(ms?: number) {
  if (!ms) return "-";
  if (ms < 1000) return `${ms}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.round(s / 60)}m`;
}

function PoolForm({ pool, onSaved }: { pool: Pool; onSaved: () => void }) {
  const [form, setForm] = useState(pool);
  useEffect(() => { setForm(pool); }, [pool]);
  const set = (k: keyof Pool, v: number | boolean) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <form
      className="space-y-4"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          await api.put("/api/v1/admin/workers", {
            pools: {
              [pool.id]: {
                enabled: pool.reserved ? true : form.enabled,
                min_workers: form.min_workers,
                max_workers: form.max_workers,
                queue_limit: form.queue_limit,
                timeout_seconds: form.timeout_seconds,
                priority: form.priority,
                max_rss_mb: form.max_rss_mb
              }
            }
          });
          toast.success(`${pool.name} saved`);
          onSaved();
        } catch (err) {
          toast.error(err instanceof Error ? err.message : "Could not save pool");
        }
      }}
    >
      <p className="text-sm text-muted">{pool.description}</p>
      {pool.reserved ? (
        <p className="text-xs text-subtle">Reserved pool: always on with at least one worker so playback and search stay responsive.</p>
      ) : (
        <SettingSwitch label="Pool enabled" description="Paused pools keep their queue until turned back on." checked={form.enabled} onChange={(v) => set("enabled", v)} />
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Minimum workers" hint={pool.reserved ? "At least 1" : "0 lets the pool sleep when idle"}>
          <Input type="number" min={pool.reserved ? 1 : 0} max={32} value={form.min_workers} onChange={(e) => set("min_workers", Number(e.target.value))} />
        </Field>
        <Field label="Maximum workers">
          <Input type="number" min={pool.reserved ? 1 : 0} max={32} value={form.max_workers} onChange={(e) => set("max_workers", Number(e.target.value))} />
        </Field>
        <Field label="Queue limit">
          <Input type="number" min={8} max={10000} value={form.queue_limit} onChange={(e) => set("queue_limit", Number(e.target.value))} />
        </Field>
        <Field label="Job timeout (seconds)">
          <Input type="number" min={5} max={7200} value={form.timeout_seconds} onChange={(e) => set("timeout_seconds", Number(e.target.value))} />
        </Field>
        <Field label="Priority" hint="Higher runs first in this pool">
          <Input type="number" min={1} max={100} value={form.priority} onChange={(e) => set("priority", Number(e.target.value))} />
        </Field>
        <Field label="Memory guide (MB)" hint="A hint only, not an enforced limit. 0 leaves it unset.">
          <Input type="number" min={0} max={65536} value={form.max_rss_mb || 0} onChange={(e) => set("max_rss_mb", Number(e.target.value))} />
        </Field>
      </div>
      <Button type="submit">Save</Button>
    </form>
  );
}

const jobFilters = [
  { value: "all", label: "All jobs" },
  { value: "active", label: "Queued and running" },
  { value: "failed", label: "Failed" },
  { value: "completed", label: "Completed" },
  { value: "cancelled", label: "Cancelled" }
];

function matchesJob(j: JobRow, filter: string, text: string) {
  if (text && !`${j.type} ${j.pool} ${j.last_error || ""}`.toLowerCase().includes(text.toLowerCase())) return false;
  switch (filter) {
    case "active":
      return j.status === "queued" || j.status === "retry" || j.status === "running";
    case "all":
      return true;
    default:
      return j.status === filter;
  }
}

export function JobsTab() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["admin-workers"],
    queryFn: () => api.get<Workers>("/api/v1/admin/workers"),
    refetchInterval: 4000
  });
  const [editing, setEditing] = useState<Pool | null>(null);
  const [filter, setFilter] = useState("all");
  const [text, setText] = useState("");
  const pools = q.data?.pools || [];
  const jobs = (q.data?.jobs || []).filter((j) => matchesJob(j, filter, text));
  const refresh = () => qc.invalidateQueries({ queryKey: ["admin-workers"] });

  return (
    <div>
      <p className="mb-3 text-sm text-muted">Background work runs in separate pools so downloads, imports, and scans never slow down playback or search.</p>
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted">
            <tr><th className="p-3">Pool</th><th className="p-3">Workers</th><th className="p-3">Queue</th><th className="p-3">Running</th><th className="p-3">Failed (24h)</th><th className="p-3">Avg time</th><th /></tr>
          </thead>
          <tbody>
            {pools.map((p) => {
              const live = p.live || ({} as Live);
              return (
                <tr key={p.id} className="border-t border-border">
                  <td className="p-3">
                    <div className="flex flex-wrap items-center gap-2 font-medium">
                      {p.name}
                      {!p.enabled && <Badge tone="warning">Paused</Badge>}
                    </div>
                  </td>
                  <td className="p-3">{live.active_workers ?? 0} <span className="text-xs text-muted">({live.busy ?? 0} busy)</span></td>
                  <td className="p-3">{live.queue_depth ?? 0}{live.oldest_queued_at ? <span className="text-xs text-muted"> · oldest {relativeTime(live.oldest_queued_at)}</span> : null}</td>
                  <td className="p-3">{live.running ?? 0}</td>
                  <td className={live.failed ? "p-3 text-destructive" : "p-3"}>{live.failed ?? 0}</td>
                  <td className="p-3">{formatMs(live.avg_duration_ms)}</td>
                  <td className="p-3 text-right"><Button size="sm" variant="ghost" onClick={() => setEditing(p)}>Configure</Button></td>
                </tr>
              );
            })}
            {!pools.length && <tr><td className="p-3 text-muted" colSpan={7}>{q.isLoading ? "Loading…" : "No worker pools reported."}</td></tr>}
          </tbody>
        </table>
      </div>

      <div className="mb-3 mt-8 flex flex-wrap items-center gap-2">
        <h3 className="mr-auto text-lg font-semibold">Jobs</h3>
        <Input className="w-56" placeholder="Filter by type or error" value={text} onChange={(e) => setText(e.target.value)} aria-label="Filter jobs" />
        <Select className="w-48" value={filter} onValueChange={setFilter} options={jobFilters} />
      </div>
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted">
            <tr><th className="p-3">Type</th><th className="p-3">Pool</th><th className="p-3">State</th><th className="p-3">Progress</th><th className="p-3">Age</th><th className="p-3">Error</th><th /></tr>
          </thead>
          <tbody>
            {jobs.length === 0 && <tr><td className="p-3 text-muted" colSpan={7}>No jobs match.</td></tr>}
            {jobs.map((j) => (
              <tr key={j.id} className="border-t border-border">
                <td className="p-3">{j.type}</td>
                <td className="p-3 text-muted">{j.pool}</td>
                <td className="p-3"><Badge tone={jobTone(j.status)}>{j.status}</Badge></td>
                <td className="w-32 p-3">
                  <div className="h-1.5 rounded-full bg-surface-3">
                    <div className="h-full rounded-full bg-accent" style={{ width: `${j.progress || 0}%` }} />
                  </div>
                </td>
                <td className="p-3 text-muted">{relativeTime(j.started_at || j.created_at)}</td>
                <td className="max-w-xs truncate p-3 text-destructive" title={j.last_error || undefined}>{j.last_error}</td>
                <td className="whitespace-nowrap p-3">
                  {(j.status === "queued" || j.status === "retry" || j.status === "running") && j.cancellable && (
                    <Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/jobs/${j.id}/cancel`).then(() => { toast("Cancel requested"); refresh(); })}>Cancel</Button>
                  )}
                  {(j.status === "failed" || j.status === "cancelled") && (
                    <Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/jobs/${j.id}/retry`).then(() => { toast.success("Queued again"); refresh(); })}>Retry</Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Dialog open={!!editing} onOpenChange={(v) => { if (!v) setEditing(null); }}>
        <DialogContent title={editing ? `${editing.name} pool` : "Pool"} className="max-h-[90vh] overflow-y-auto">
          {editing && <PoolForm pool={editing} onSaved={() => { setEditing(null); refresh(); }} />}
        </DialogContent>
      </Dialog>
    </div>
  );
}
