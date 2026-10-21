import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Settings2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Select } from "@/components/ui/select";
import { cn, relativeTime } from "@/lib/utils";
import { SettingSwitch, errorMessage } from "../adminUi";

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

const jobFilters = [
  { value: "all", label: "All jobs" },
  { value: "active", label: "Queued and running" },
  { value: "failed", label: "Failed" },
  { value: "completed", label: "Completed" },
  { value: "cancelled", label: "Cancelled" }
];
const jobFilterValues = jobFilters.map((f) => f.value);

const isActive = (j: JobRow) => j.status === "queued" || j.status === "retry" || j.status === "running";

function matchesJob(j: JobRow, filter: string, text: string) {
  if (text && !`${j.type} ${j.pool} ${j.last_error || ""}`.toLowerCase().includes(text.toLowerCase())) return false;
  if (filter === "all") return true;
  if (filter === "active") return isActive(j);
  return j.status === filter;
}

/** Worker pools as a collapsible strip, then the jobs table. */
export function JobsPanel() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-workers"], queryFn: () => api.get<Workers>("/api/v1/admin/workers"), refetchInterval: 4000 });
  const [params, setParams] = useSearchParams();
  const raw = params.get("jobs") || "all";
  const filter = jobFilterValues.includes(raw) ? raw : "all";
  const setFilter = (v: string) => {
    const next = new URLSearchParams(params);
    if (v === "all") next.delete("jobs");
    else next.set("jobs", v);
    setParams(next, { replace: true });
  };
  const [text, setText] = useState("");
  const [editing, setEditing] = useState<Pool | null>(null);
  const pools = q.data?.pools || [];
  const jobs = (q.data?.jobs || []).filter((j) => matchesJob(j, filter, text));
  const refresh = () => qc.invalidateQueries({ queryKey: ["admin-workers"] });

  async function act(j: JobRow, kind: "cancel" | "retry") {
    try {
      await api.post(`/api/v1/admin/jobs/${j.id}/${kind}`);
      if (kind === "cancel") toast("Cancel requested");
      else toast.success("Queued again");
      refresh();
    } catch (e) {
      toast.error(errorMessage(e, kind === "cancel" ? "Could not cancel the job" : "Could not retry the job"));
    }
  }

  return (
    <section>
      <PoolStrip pools={pools} loading={q.isLoading} onConfigure={setEditing} />

      <div className="mb-3 mt-6 flex flex-wrap items-center gap-2">
        <h2 className="mr-auto text-base font-semibold">Jobs</h2>
        <Input className="min-w-0 flex-1 basis-40 sm:w-56 sm:flex-none" placeholder="Filter by type or error" value={text} onChange={(e) => setText(e.target.value)} aria-label="Filter jobs" />
        <Select className="w-full sm:w-48" value={filter} onValueChange={setFilter} options={jobFilters} />
      </div>
      <div className="overflow-hidden rounded-xl border border-border">
        <ul className="divide-y divide-border">
          {jobs.map((j) => (
            <li key={j.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 text-sm">
              <div className="min-w-0 flex-1 basis-56">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{j.type}</span>
                  <Badge tone={jobTone(j.status)}>{j.status}</Badge>
                  <span className="text-xs text-subtle">{j.pool} · {relativeTime(j.started_at || j.created_at)}{j.attempts > 1 ? ` · ${j.attempts} attempts` : ""}</span>
                </div>
                {j.last_error && <p className="mt-0.5 truncate text-xs text-destructive" title={j.last_error}>{j.last_error}</p>}
                {j.status === "running" && (
                  <div className="mt-1.5 h-1 max-w-xs overflow-hidden rounded-full bg-surface-3">
                    <div className="h-full rounded-full bg-accent" style={{ width: `${j.progress || 0}%` }} />
                  </div>
                )}
              </div>
              {isActive(j) && j.cancellable && (
                <Button size="sm" variant="ghost" onClick={() => void act(j, "cancel")}>Cancel</Button>
              )}
              {(j.status === "failed" || j.status === "cancelled") && (
                <Button size="sm" variant="secondary" onClick={() => void act(j, "retry")}>Retry</Button>
              )}
            </li>
          ))}
          {!jobs.length && <li className="px-4 py-8 text-center text-sm text-muted">{q.isLoading ? "Loading…" : "No jobs match."}</li>}
        </ul>
      </div>

      <Dialog open={!!editing} onOpenChange={(v) => { if (!v) setEditing(null); }}>
        <DialogContent title={editing ? `${editing.name} pool` : "Pool"} className="max-h-[90vh] overflow-y-auto">
          {editing && <PoolForm pool={editing} onSaved={() => { setEditing(null); refresh(); }} />}
        </DialogContent>
      </Dialog>
    </section>
  );
}

function PoolStrip({ pools, loading, onConfigure }: { pools: Pool[]; loading: boolean; onConfigure: (p: Pool) => void }) {
  const [open, setOpen] = useState(false);
  const sum = (k: keyof Live) => pools.reduce((n, p) => n + (Number(p.live?.[k]) || 0), 0);
  const busy = sum("busy");
  const queued = sum("queue_depth");
  const failed = sum("failed");
  const paused = pools.filter((p) => !p.enabled).length;
  return (
    <div className="rounded-2xl border border-border bg-surface-1 shadow-sm">
      <button type="button" className="flex w-full flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 text-left" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <span className="text-sm font-semibold">Worker pools</span>
        <span className="flex min-w-0 flex-1 flex-wrap gap-x-3 gap-y-1 text-xs text-muted">
          {loading ? (
            <span>Loading…</span>
          ) : (
            <>
              <span>{pools.length} pools</span>
              <span>{busy} busy</span>
              <span>{queued} queued</span>
              {failed > 0 && <span className="text-destructive">{failed} failed today</span>}
              {paused > 0 && <span className="text-warning">{paused} paused</span>}
            </>
          )}
        </span>
        <ChevronDown className={cn("h-4 w-4 shrink-0 text-muted transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <div className="grid gap-2 border-t border-border p-3 sm:grid-cols-2 xl:grid-cols-3">
          {pools.map((p) => {
            const live = p.live || ({} as Live);
            return (
              <div key={p.id} className="flex min-w-0 items-center gap-3 rounded-xl bg-surface-2/50 px-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <span className="truncate">{p.name}</span>
                    {!p.enabled && <Badge tone="warning">Paused</Badge>}
                  </div>
                  <div className="truncate text-xs text-subtle">
                    {live.busy ?? 0}/{live.active_workers ?? 0} busy · {live.queue_depth ?? 0} queued
                    {live.oldest_queued_at ? ` (oldest ${relativeTime(live.oldest_queued_at)})` : ""} · avg {formatMs(live.avg_duration_ms)}
                    {live.failed ? <span className="text-destructive"> · {live.failed} failed</span> : null}
                  </div>
                </div>
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" aria-label={`Configure ${p.name}`} title="Configure" onClick={() => onConfigure(p)}>
                  <Settings2 />
                </Button>
              </div>
            );
          })}
          {!pools.length && <p className="px-1 text-sm text-muted">No worker pools reported.</p>}
          <p className="px-1 text-xs text-subtle sm:col-span-2 xl:col-span-3">Each pool runs its own work so downloads, imports, and scans never slow down playback or search.</p>
        </div>
      )}
    </div>
  );
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
          toast.error(errorMessage(err, "Could not save pool"));
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
