import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { api } from "@/lib/api";
import { Badge, Progress } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/empty";
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

export function AdminStatsRebuild() {
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
      <PageHeader
        title="Stats rebuild"
        description="Queue a one-time cutover job. Production Home, Stats, and Wrapped keep reading listen_history until this rebuild finishes and flips the reader to listen_events. This page is rebuild plus current reader mode - not a merged listen total."
      />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Badge tone={readerTone(mode)}>{onEvents ? "Reader: listen_events" : "Reader: listen_history"}</Badge>
        {busy && <Badge tone="accent">Rebuild in progress</Badge>}
        {job && !busy && <Badge tone={jobTone(job.status)}>Last job: {job.status}</Badge>}
      </div>

      <article className="mb-6 rounded-xl border border-border bg-surface-1 p-4 text-sm">
        <div className="mb-2 flex items-center gap-2 font-medium">
          <RefreshCw className="h-4 w-4 text-muted" />
          Cutover
        </div>
        <p className="text-muted">
          Listen events are written in the background today. Readers do not switch to{" "}
          <span className="font-medium text-foreground">listen_events</span> until a successful{" "}
          <span className="font-medium text-foreground">stats.rebuild</span> job completes. Until then, counts stay on{" "}
          <span className="font-medium text-foreground">listen_history</span>. Cancel is hidden during the swap - let the
          job finish.
        </p>
        <p className="mt-2 text-muted">
          Side-by-side history vs events validation (not a combined total) lives on{" "}
          <Link to="/admin/listen-compare" className="text-accent hover:underline">
            Listen compare
          </Link>
          .
        </p>
      </article>

      {q.isError && (
        <p className="mb-4 text-sm text-destructive">{q.error instanceof Error ? q.error.message : "Could not load rebuild status"}</p>
      )}

      <section className="mb-6 rounded-xl border border-border bg-surface-1 p-4">
        <h2 className="mb-3 font-semibold">Current reader</h2>
        <dl className="space-y-2 text-sm">
          <div className="flex items-start justify-between gap-4">
            <dt className="text-muted">
              listen_reader
              <div className="text-xs text-subtle">server_settings key; missing is treated as history</div>
            </dt>
            <dd className="font-medium">{mode}</dd>
          </div>
          <div className="flex items-start justify-between gap-4">
            <dt className="text-muted">Home / Stats / Wrapped</dt>
            <dd className="font-medium">{onEvents ? "listen_events" : "listen_history"}</dd>
          </div>
        </dl>
      </section>

      <section className="rounded-xl border border-border bg-surface-1 p-4">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">Rebuild job</h2>
          <Button size="sm" onClick={enqueue} disabled={busy || submitting}>
            {busy ? "Rebuild running" : "Queue rebuild"}
          </Button>
        </div>
        {!job ? (
          <p className="text-sm text-muted">No stats.rebuild job has been queued yet.</p>
        ) : (
          <div className="space-y-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={jobTone(job.status)}>{job.status}</Badge>
              <span className="text-muted">id {job.id}</span>
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
