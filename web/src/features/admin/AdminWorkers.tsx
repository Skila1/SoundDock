import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
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

function PoolCard({ pool, onSaved }: { pool: Pool; onSaved: () => void }) {
  const [form, setForm] = useState(pool);
  useEffect(() => { setForm(pool); }, [pool]);
  const live = pool.live || ({} as Live);
  const set = (k: keyof Pool, v: number | boolean) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <article className="rounded-xl border border-border bg-surface-1 p-4">
      <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="font-semibold">{pool.name}</h2>
            {pool.reserved && <Badge tone="success">Reserved</Badge>}
            <Badge tone={form.enabled ? "success" : "warning"}>{form.enabled ? "On" : "Off"}</Badge>
          </div>
          <p className="mt-1 max-w-xl text-sm text-muted">{pool.description}</p>
        </div>
        {pool.reserved ? (
          <div className="text-xs text-subtle">Always on · at least one worker</div>
        ) : (
          <label className="flex items-center gap-2 text-sm">
            Enabled
            <Switch checked={form.enabled} onCheckedChange={(v) => set("enabled", v)} />
          </label>
        )}
      </div>
      <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div>
          <div className="text-xs text-subtle">Workers</div>
          <div className="text-lg font-semibold">{live.active_workers ?? 0}</div>
          <div className="text-xs text-muted">{live.busy ?? 0} busy · {live.idle ?? 0} idle</div>
        </div>
        <div>
          <div className="text-xs text-subtle">Queue</div>
          <div className="text-lg font-semibold">{live.queue_depth ?? 0}</div>
          <div className="text-xs text-muted">{live.oldest_queued_at ? `oldest ${relativeTime(live.oldest_queued_at)}` : "empty"}</div>
        </div>
        <div>
          <div className="text-xs text-subtle">Running / failed</div>
          <div className="text-lg font-semibold">{live.running ?? 0} / {live.failed ?? 0}</div>
          <div className="text-xs text-muted">failed in 24h</div>
        </div>
        <div>
          <div className="text-xs text-subtle">Avg duration</div>
          <div className="text-lg font-semibold">{formatMs(live.avg_duration_ms)}</div>
          <div className="text-xs text-muted">completed in 24h</div>
        </div>
      </div>
      <form
        className="grid gap-3 sm:grid-cols-3"
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
        <Field label="Minimum workers" hint={pool.reserved ? "Cannot be zero" : "0 is allowed while the pool is idle"}>
          <Input type="number" min={pool.reserved ? 1 : 0} max={32} value={form.min_workers}
            onChange={(e) => set("min_workers", Number(e.target.value))} />
        </Field>
        <Field label="Maximum workers">
          <Input type="number" min={pool.reserved ? 1 : 0} max={32} value={form.max_workers}
            onChange={(e) => set("max_workers", Number(e.target.value))} />
        </Field>
        <Field label="Queue limit">
          <Input type="number" min={8} max={10000} value={form.queue_limit}
            onChange={(e) => set("queue_limit", Number(e.target.value))} />
        </Field>
        <Field label="Job timeout (seconds)">
          <Input type="number" min={5} max={7200} value={form.timeout_seconds}
            onChange={(e) => set("timeout_seconds", Number(e.target.value))} />
        </Field>
        <Field label="Priority" hint="Higher is claimed first inside this pool">
          <Input type="number" min={1} max={100} value={form.priority}
            onChange={(e) => set("priority", Number(e.target.value))} />
        </Field>
        <Field label="Memory cap (MB, advisory)" hint="Advisory only - this is not a cgroup or enforced memory limit. 0 leaves it unset. Worker min/max concurrency is what actually caps load.">
          <Input type="number" min={0} max={65536} value={form.max_rss_mb || 0}
            onChange={(e) => set("max_rss_mb", Number(e.target.value))} />
        </Field>
        <div className="sm:col-span-3">
          <Button type="submit" size="sm">Save {pool.name}</Button>
        </div>
      </form>
    </article>
  );
}

export function AdminWorkers() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["admin-workers"],
    queryFn: () => api.get<Workers>("/api/v1/admin/workers"),
    refetchInterval: 4000
  });
  const pools = q.data?.pools || [];
  const jobs = q.data?.jobs || [];
  const refresh = () => qc.invalidateQueries({ queryKey: ["admin-workers"] });

  return (
    <div>
      <PageHeader
        title="Workers"
        description="Workload pools with reserved capacity for playback and search. Downloads, Spotify imports, scans, merges, and deletes run in the background and cannot starve the API."
      />
      <div className="space-y-4">
        {pools.map((p) => (
          <PoolCard key={p.id} pool={p} onSaved={refresh} />
        ))}
      </div>
      <h2 className="mb-3 mt-8 text-lg font-semibold">Jobs</h2>
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted">
            <tr>
              <th className="p-3">Type</th>
              <th className="p-3">Pool</th>
              <th className="p-3">State</th>
              <th className="p-3">Progress</th>
              <th className="p-3">Age</th>
              <th className="p-3">Error</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {jobs.length === 0 && (
              <tr><td className="p-3 text-muted" colSpan={7}>No jobs yet</td></tr>
            )}
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
                <td className="max-w-xs truncate p-3 text-destructive">{j.last_error}</td>
                <td className="whitespace-nowrap p-3">
                  {(j.status === "queued" || j.status === "retry" || j.status === "running") && j.cancellable && (
                    <Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/jobs/${j.id}/cancel`).then(() => { toast("Cancel requested"); refresh(); })}>
                      Cancel
                    </Button>
                  )}
                  {(j.status === "failed" || j.status === "cancelled") && (
                    <Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/jobs/${j.id}/retry`).then(() => { toast.success("Queued again"); refresh(); })}>
                      Retry
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
