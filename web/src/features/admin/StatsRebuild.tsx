import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { api } from "@/lib/api";
import { Badge, Progress } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { relativeTime } from "@/lib/utils";
import { toast } from "sonner";
import type { StatsRebuildEnqueue, StatsRebuildStatus } from "@/types/api";

function readerTone(mode?: string) {
  if (mode === "events") return "success" as const;
  return "warning" as const;
}

function jobTone(status?: string) {
  if (status === "failed") return "danger" as const;
  if (status === "completed") return "success" as const;
  if (status === "cancelled") return "warning" as const;
  if (status === "running") return "accent" as const;
  return "neutral" as const;
}

export function StatsRebuildPanel() {
  const qc = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const q = useQuery({
    queryKey: ["admin-stats-rebuild"],
    queryFn: () => api.get<StatsRebuildStatus>("/api/v1/admin/stats/rebuild"),
    refetchInterval: (query) => (query.state.data?.busy ? 2000 : 8000)
  });

  const d = q.data;
  const mode = d?.listen_reader || "history";
  const onEvents = mode === "events";
  const job = d?.job;
  const busy = !!d?.busy;

  async function enqueue() {
    setSubmitting(true);
    try {
      await api.post<StatsRebuildEnqueue>("/api/v1/admin/stats/rebuild");
      toast.success("Stats rebuild queued");
      await qc.invalidateQueries({ queryKey: ["admin-stats-rebuild"] });
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 409) {
        toast.error("A rebuild is already queued or running");
        await q.refetch();
      } else {
        toast.error(err.message || "Could not enqueue rebuild");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div>

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Badge tone={readerTone(mode)}>{onEvents ? "Using new listening data" : "Using older history"}</Badge>
        {busy && <Badge tone="accent">Rebuild in progress</Badge>}
        {job && !busy && <Badge tone={jobTone(job.status)}>Last job: {job.status}</Badge>}
      </div>

      <article className="mb-6 rounded-2xl border border-border bg-surface-1 p-5 shadow-sm text-sm">
        <div className="mb-2 flex items-center gap-2 font-medium">
          <RefreshCw className="h-4 w-4 text-muted" />
          Cutover
        </div>
        <p className="text-muted">
          New listening data is recorded in a more detailed format in the background. Home and Stats keep using the
          older history until this one-time rebuild finishes and switches them over. Let the rebuild finish once it starts.
        </p>
        <p className="mt-2 text-muted">
          To check the two sources agree first, use the{" "}
          <Link to="/admin/stats-migration?tab=compare" className="text-accent hover:underline">
            Compare
          </Link>{" "}
          tab.
        </p>
      </article>

      {q.isError && (
        <p className="mb-4 text-sm text-destructive">{q.error instanceof Error ? q.error.message : "Could not load rebuild status"}</p>
      )}

      <section className="mb-6 rounded-2xl border border-border bg-surface-1 p-5 shadow-sm">
        <h2 className="mb-3 font-semibold">Current reader</h2>
        <dl className="space-y-2 text-sm">
          <div className="flex items-start justify-between gap-4">
            <dt className="text-muted">
              Stats source
            </dt>
            <dd className="font-medium">{mode}</dd>
          </div>
          <div className="flex items-start justify-between gap-4">
            <dt className="text-muted">Home and Stats read from</dt>
            <dd className="font-medium">{onEvents ? "New listening data" : "Older history"}</dd>
          </div>
        </dl>
      </section>

      <section className="rounded-2xl border border-border bg-surface-1 p-5 shadow-sm">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-base font-semibold tracking-tight">Rebuild job</h2>
          <Button size="sm" onClick={enqueue} disabled={busy || submitting}>
            {busy ? "Rebuild running" : "Start rebuild"}
          </Button>
        </div>
        {!job ? (
          <p className="text-sm text-muted">The rebuild has not been run yet.</p>
        ) : (
          <div className="space-y-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={jobTone(job.status)}>{job.status}</Badge>
              
            </div>
            {(busy || job.progress > 0) && <Progress value={job.progress || 0} />}
            <dl className="space-y-2">
              <div className="flex justify-between gap-4">
                <dt className="text-muted">Queued</dt>
                <dd className="text-muted">{relativeTime(job.created_at)}</dd>
              </div>
              {job.started_at && (
                <div className="flex justify-between gap-4">
                  <dt className="text-muted">Started</dt>
                  <dd className="text-muted">{relativeTime(job.started_at)}</dd>
                </div>
              )}
              {job.finished_at && (
                <div className="flex justify-between gap-4">
                  <dt className="text-muted">Finished</dt>
                  <dd className="text-muted">{relativeTime(job.finished_at)}</dd>
                </div>
              )}
            </dl>
            {job.last_error && <p className="text-destructive">{job.last_error}</p>}
          </div>
        )}
      </section>
    </div>
  );
}
