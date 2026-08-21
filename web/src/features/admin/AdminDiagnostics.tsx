import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { formatBytes } from "@/lib/utils";

type Diag = {
  go?: { version?: string; goroutines?: number; cpus?: number; heap_alloc?: number; sys?: number };
  dirs?: Record<string, { path?: string; bytes?: number; files?: number; ok?: boolean; error?: string }>;
  binaries?: Record<string, boolean | string>;
  postgres?: boolean;
  database_size?: string;
  failed_jobs?: number;
  last_backup?: { id?: string; status?: string; created_at?: string };
  active_streams?: number;
  worker?: boolean;
  fingerprint?: string;
  maintenance?: boolean;
};

export function AdminDiagnostics() {
  const q = useQuery({ queryKey: ["admin-diagnostics"], queryFn: () => api.get<Diag>("/api/v1/admin/diagnostics"), refetchInterval: 15000 });
  const d = q.data || {};
  const dirs = d.dirs || {};
  const bins = d.binaries || {};
  return (
    <div>
      <PageHeader title="Diagnostics" description="Runtime, disk, and tool checks for this instance." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.postgres ? "success" : "danger"}>{d.postgres ? "Postgres" : "Postgres down"}</Badge>
        <Badge tone={d.worker ? "success" : "warning"}>{d.worker ? "Worker" : "Draining"}</Badge>
        <Badge tone={d.fingerprint === "available" ? "success" : "warning"}>fpcalc {d.fingerprint || "unknown"}</Badge>
        <Badge tone={d.maintenance ? "warning" : "neutral"}>{d.maintenance ? "Maintenance" : "Live"}</Badge>
      </div>
      <div className="mb-4 grid gap-3 md:grid-cols-3">
        <Stat label="Go" value={d.go?.version || "—"} hint={`${d.go?.goroutines ?? "—"} goroutines · ${d.go?.cpus ?? "—"} CPUs`} />
        <Stat label="Heap" value={formatBytes(d.go?.heap_alloc)} hint={`sys ${formatBytes(d.go?.sys)}`} />
        <Stat label="Database" value={d.database_size || "—"} hint={`${d.failed_jobs ?? 0} failed jobs · ${d.active_streams ?? 0} streams`} />
      </div>
      <h3 className="mb-2 font-semibold">Directories</h3>
      <div className="mb-6 grid gap-3 md:grid-cols-2">
        {Object.entries(dirs).map(([k, v]) => (
          <article key={k} className="rounded-xl border border-border bg-surface-1 p-4">
            <div className="text-sm capitalize text-muted">{k}</div>
            <div className="mt-1 font-semibold">{v.ok ? formatBytes(v.bytes) : v.error || "unavailable"}</div>
            <div className="mt-1 truncate text-xs text-subtle">{v.path} · {v.files ?? 0} files</div>
          </article>
        ))}
      </div>
      <h3 className="mb-2 font-semibold">Binaries</h3>
      <div className="flex flex-wrap gap-2">
        {Object.entries(bins).map(([k, v]) => (
          <Badge key={k} tone={v === true || v === "available" ? "success" : "warning"}>{k}: {String(v)}</Badge>
        ))}
      </div>
      {d.last_backup?.status && (
        <p className="mt-4 text-sm text-muted">Last backup: {d.last_backup.status}{d.last_backup.created_at ? ` · ${d.last_backup.created_at}` : ""}</p>
      )}
    </div>
  );
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <article className="rounded-xl border border-border bg-surface-1 p-4">
      <div className="text-sm text-muted">{label}</div>
      <div className="mt-1 text-xl font-semibold">{value}</div>
      {hint && <div className="mt-1 text-xs text-subtle">{hint}</div>}
    </article>
  );
}
