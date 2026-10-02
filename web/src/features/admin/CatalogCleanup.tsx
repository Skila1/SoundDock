import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { relativeTime } from "@/lib/utils";

function metadataJobTone(status?: string) {
  if (status === "failed") return "danger" as const;
  if (status === "completed") return "success" as const;
  if (status === "cancelled") return "warning" as const;
  if (status === "running") return "accent" as const;
  return "neutral" as const;
}

export function CatalogCleanup() {
  const qc = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const q = useQuery({
    queryKey: ["admin-meta"],
    queryFn: () => api.get<{
      external_enabled?: boolean;
      providers?: string[];
      track_count?: number;
      busy?: boolean;
      job?: {
        id: string;
        status: string;
        progress: number;
        last_error?: string | null;
        created_at: string;
        started_at?: string | null;
        finished_at?: string | null;
        result?: { total?: number; done?: number; updated?: number; skipped?: number; failed?: number; covers?: number; unmatched?: number };
      } | null;
    }>("/api/v1/admin/metadata"),
    refetchInterval: (query) => (query.state.data?.busy ? 1000 : 8000),
  });

  const d = q.data;
  const job = d?.job;
  const busy = !!d?.busy;
  const tracks = d?.track_count ?? 0;
  const result = job?.result;
  const done = result?.done ?? 0;
  const total = result?.total ?? tracks;
  const pct = typeof job?.progress === "number" ? job.progress : 0;

  async function refreshAll() {
    setSubmitting(true);
    try {
      await api.post("/api/v1/admin/metadata/refresh");
      toast.success("Metadata update started");
      await qc.invalidateQueries({ queryKey: ["admin-meta"] });
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 409) {
        toast.error("An update is already running");
        await q.refetch();
      } else {
        toast.error(err.message || "Could not start metadata update");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div>
      <section className="rounded-xl border border-border bg-surface-1 p-4">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="font-semibold">Fill in missing metadata</h2>
            <p className="mt-1 text-sm text-muted">
              Looks up every track on MusicBrainz and fills in missing tags, genres, artist links, and cover art. Tracks you have locked are skipped.
              {tracks > 0 && (
                <> About {tracks} track{tracks === 1 ? "" : "s"} to check. MusicBrainz allows one lookup per second, so this can take a while.</>
              )}
            </p>
          </div>
          <Button onClick={refreshAll} disabled={busy || submitting}>
            {busy ? "Updating…" : submitting ? "Starting…" : "Update metadata"}
          </Button>
        </div>
        {job ? (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <Badge tone={metadataJobTone(job.status)}>{job.status}</Badge>
              {busy && <Badge tone="accent">In progress</Badge>}
              <span className="text-muted">
                {busy || done > 0 ? `${done} / ${total || tracks} tracks` : `${tracks} tracks in library`}
              </span>
              <span className="text-muted">{pct}%</span>
            </div>
            <div className="h-3 w-full overflow-hidden rounded-full bg-surface-3">
              <div className="h-full bg-accent transition-all" style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} />
            </div>
            <dl className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
              {result?.updated != null && (
                <div><dt className="text-muted">Updated</dt><dd className="font-medium">{result.updated}</dd></div>
              )}
              {result?.covers != null && (
                <div><dt className="text-muted">Covers added</dt><dd className="font-medium">{result.covers}</dd></div>
              )}
              {result?.skipped != null && (
                <div><dt className="text-muted">Skipped</dt><dd className="font-medium">{result.skipped}</dd></div>
              )}
              {result?.failed != null && (
                <div><dt className="text-muted">Failed</dt><dd className="font-medium">{result.failed}</dd></div>
              )}
            </dl>
            <p className="text-xs text-subtle">Queued {relativeTime(job.created_at)}</p>
            {job.last_error && <p className="text-sm text-destructive">{job.last_error}</p>}
          </div>
        ) : (
          <p className="text-sm text-muted">Not run yet.{d && d.external_enabled === false ? " Turn on online metadata under Media Settings first." : ""}</p>
        )}
      </section>
    </div>
  );
}

