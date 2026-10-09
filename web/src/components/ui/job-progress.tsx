import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { create } from "zustand";
import { api } from "@/lib/api";
import { TaskProgress } from "./task-progress";

type JobView = {
  id: string;
  type: string;
  status: string;
  progress: number;
  error?: string | null;
  result?: { done?: number; total?: number; matched?: number; unmatched?: number; added?: number; playlist_id?: string };
};

type Tracked = { id: string; label: string };

/** Jobs the user started in this tab whose progress should stay visible. */
export const useTrackedJobs = create<{ jobs: Tracked[]; track: (id: string, label: string) => void; drop: (id: string) => void }>((set) => ({
  jobs: [],
  track: (id, label) => set((s) => (s.jobs.some((j) => j.id === id) ? s : { jobs: [...s.jobs, { id, label }] })),
  drop: (id) => set((s) => ({ jobs: s.jobs.filter((j) => j.id !== id) }))
}));

const finished = (status?: string) => status === "completed" || status === "failed" || status === "cancelled" || status === "dead";

function JobRow({ job, onDone }: { job: Tracked; onDone?: () => void }) {
  const qc = useQueryClient();
  const drop = useTrackedJobs((s) => s.drop);
  const q = useQuery({
    queryKey: ["my-job", job.id],
    queryFn: () => api.get<JobView>(`/api/v1/me/jobs/${job.id}`),
    refetchInterval: (query) => (finished(query.state.data?.status) ? false : 1500)
  });
  const d = q.data;
  const done = finished(d?.status);
  useEffect(() => {
    if (!done) return;
    void qc.invalidateQueries({ queryKey: ["playlists"] });
    void qc.invalidateQueries({ queryKey: ["playlist"] });
    onDone?.();
  }, [done, qc, onDone]);
  const r = d?.result || {};
  const total = r.total || 0;
  const failed = d?.status === "failed" || d?.status === "dead";
  return (
    <TaskProgress
      task={{
        label: job.label,
        done: total ? r.done || 0 : d?.progress || 0,
        total: total || 100,
        failed: done ? r.unmatched : undefined,
        status: failed ? "error" : done ? "done" : "running",
        detail: failed
          ? d?.error || "Import failed"
          : r.matched != null
            ? `${r.matched} matched in your library${done && r.added != null ? ` · ${r.added} in the playlist` : ""}${!done && d?.status === "queued" ? " · waiting for a worker" : ""}`
            : d?.status === "queued"
              ? "Waiting for a worker"
              : undefined
      }}
      onDismiss={() => drop(job.id)}
    />
  );
}

/** Live progress cards for every tracked job. */
export function TrackedJobs({ className }: { className?: string }) {
  const jobs = useTrackedJobs((s) => s.jobs);
  if (!jobs.length) return null;
  return (
    <div className={className}>
      <div className="space-y-2">
        {jobs.map((j) => (
          <JobRow key={j.id} job={j} />
        ))}
      </div>
    </div>
  );
}
