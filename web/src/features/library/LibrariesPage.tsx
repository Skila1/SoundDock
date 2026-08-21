import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Library as LibIcon } from "lucide-react";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import type { Library } from "@/types/api";
import { toast } from "sonner";
import type { User } from "@/types/api";
import { ScanProgressBar, latestScan, scanActive, useScanRuns } from "@/features/library/ScanProgress";

export function LibrariesPage({ user }: { user: User }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const scans = useScanRuns(!!user.is_admin);
  return (
    <div>
      <PageHeader title="Libraries" description="Local folders, object storage, and managed collections." />
      {!q.data?.length && !q.isLoading && (
        <EmptyState icon={LibIcon} title="No libraries attached." description="Create a library from Administration to scan existing music." />
      )}
      <div className="grid gap-4 md:grid-cols-2">
        {(q.data || []).map((l) => (
          <article key={l.id} className="rounded-xl border border-border bg-surface-1 p-5 shadow-card">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="text-lg font-semibold">{l.name}</h2>
                <p className="text-sm text-muted capitalize">{l.kind} · {l.storage_type || "storage"} · {l.organisation_mode}</p>
              </div>
              <Badge tone={l.read_only ? "warning" : "success"}>{l.read_only ? "Read-only" : "Writable"}</Badge>
            </div>
            <p className="mt-4 text-2xl font-semibold">{l.track_count ?? "-"} <span className="text-sm font-normal text-muted">tracks</span></p>
            {user.is_admin && (
              <div className="mt-4">
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={scanActive(latestScan(scans.data, l.id))}
                    onClick={async () => {
                      await api.post(`/api/v1/admin/libraries/${l.id}/scan`);
                      toast.success("Scan started");
                      scans.refetch();
                    }}
                  >
                    {scanActive(latestScan(scans.data, l.id)) ? "Scanning…" : "Scan"}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/libraries/${l.id}/migrate`).then(() => { toast.success("Migration started"); qc.invalidateQueries({ queryKey: ["libraries"] }); })}>Migrate</Button>
                </div>
                <ScanProgressBar scan={latestScan(scans.data, l.id)} />
              </div>
            )}
          </article>
        ))}
      </div>
    </div>
  );
}
