import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/empty";
import { formatBytes } from "@/lib/utils";

type CheckStatus = "PASS" | "WARN" | "FAIL" | "SKIP";

type Check = { id: string; name: string; status: CheckStatus; ok?: boolean; detail?: string };

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
  checks?: Check[];
  failed_checks?: number;
  warned_checks?: number;
};

function toneFor(status: CheckStatus) {
  switch (status) {
    case "PASS":
      return "success" as const;
    case "WARN":
      return "warning" as const;
    case "FAIL":
      return "danger" as const;
    default:
      return "neutral" as const;
  }
}

export function AdminDiagnostics() {
  const q = useQuery({ queryKey: ["admin-diagnostics"], queryFn: () => api.get<Diag>("/api/v1/admin/diagnostics") });
  const d = q.data || {};
  const dirs = d.dirs || {};
  const bins = d.binaries || {};
  const checks = d.checks || [];
  const failed = d.failed_checks ?? checks.filter((c) => c.status === "FAIL").length;
  const warned = d.warned_checks ?? checks.filter((c) => c.status === "WARN").length;
  return (
    <div>
      <PageHeader
        title="Diagnostics"
        description="Live probes for database, workers, disk, backups, lyrics, providers, and updates. A stored setting is never treated as a pass."
        actions={<Button variant="secondary" disabled={q.isFetching} onClick={() => q.refetch()}>{q.isFetching ? "Testing…" : "Run tests"}</Button>}
      />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.postgres ? "success" : "danger"}>{d.postgres ? "Postgres" : "Postgres down"}</Badge>
        <Badge tone={d.worker ? "success" : "warning"}>{d.worker ? "Worker" : "Draining"}</Badge>
        <Badge tone={d.fingerprint === "available" ? "success" : "warning"}>fpcalc {d.fingerprint || "unknown"}</Badge>
        <Badge tone={d.maintenance ? "warning" : "neutral"}>{d.maintenance ? "Maintenance" : "Live"}</Badge>
        {checks.length > 0 && <Badge tone={failed ? "danger" : warned ? "warning" : "success"}>{failed ? `${failed} failed` : warned ? `${warned} warnings` : "All probes passed"}</Badge>}
      </div>
      {checks.length > 0 && (
        <>
          <h3 className="mb-2 font-semibold">System tests</h3>
          <ul className="mb-6 divide-y divide-border rounded-xl border border-border bg-surface-1">
            {checks.map((c) => (
              <li key={c.id} className="flex items-start justify-between gap-3 px-4 py-3">
                <div>
                  <div className="font-medium">{c.name}</div>
                  <div className="text-sm text-muted">{c.detail}</div>
                </div>
                <Badge tone={toneFor(c.status || (c.ok ? "PASS" : "FAIL"))}>{c.status || (c.ok ? "PASS" : "FAIL")}</Badge>
              </li>
            ))}
          </ul>
        </>
      )}
      <div className="mb-4 grid gap-3 md:grid-cols-3">
        <Stat label="Go" value={d.go?.version || "-"} hint={`${d.go?.goroutines ?? "-"} goroutines · ${d.go?.cpus ?? "-"} CPUs`} />
        <Stat label="Heap" value={formatBytes(d.go?.heap_alloc)} hint={`sys ${formatBytes(d.go?.sys)}`} />
        <Stat label="Database" value={d.database_size || "-"} hint={`${d.failed_jobs ?? 0} failed jobs · ${d.active_streams ?? 0} streams`} />
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
