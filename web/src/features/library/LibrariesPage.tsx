import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Library as LibIcon } from "lucide-react";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import type { Library } from "@/types/api";
import { toast } from "sonner";
import type { User } from "@/types/api";
import { ScanProgressBar, latestScan, scanActive, useScanRuns } from "@/features/library/ScanProgress";

export function LibrariesPage({ user }: { user: User }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const scans = useScanRuns(!!user.is_admin);
  const list = q.data || [];
  const [migrateFrom, setMigrateFrom] = useState<Library | null>(null);
  const [migrateDest, setMigrateDest] = useState("");
  const destOptions = list
    .filter((l) => l.id !== migrateFrom?.id)
    .map((l) => ({ value: l.id, label: `${l.name}${l.storage_type === "managed" ? " (managed)" : ""}${l.is_default ? " · default" : ""}` }));
  return (
    <div>
      <PageHeader title="Libraries" description="Local folders, object storage, and managed collections." />
      {!q.data?.length && !q.isLoading && (
        <EmptyState icon={LibIcon} title="No libraries attached." description="Create a library from Administration to scan existing music." />
      )}
      <div className="grid gap-4 md:grid-cols-2">
        {list.map((l) => (
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
                  <Button size="sm" variant="ghost" onClick={() => {
                    const others = list.filter((x) => x.id !== l.id);
                    const preferred = others.find((x) => x.storage_type === "managed") || others.find((x) => x.is_default) || others[0];
                    setMigrateFrom(l);
                    setMigrateDest(preferred?.id || "");
                  }}>Migrate</Button>
                </div>
                <ScanProgressBar scan={latestScan(scans.data, l.id)} />
              </div>
            )}
          </article>
        ))}
      </div>
      <Dialog open={!!migrateFrom} onOpenChange={(v) => { if (!v) setMigrateFrom(null); }}>
        <DialogContent title={migrateFrom ? `Migrate ${migrateFrom.name}` : "Migrate"}>
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            if (!migrateFrom) return;
            if (!migrateDest) {
              toast.error("Choose a destination library");
              return;
            }
            await api.post(`/api/v1/admin/libraries/${migrateFrom.id}/migrate`, { dest_library_id: migrateDest });
            toast.success("Migration started");
            setMigrateFrom(null);
            qc.invalidateQueries({ queryKey: ["libraries"] });
          }}>
            <p className="text-sm text-muted">Files are copied into the destination library. A destination is required — nothing is copied without one.</p>
            <Field label="Destination library">
              <Select value={migrateDest} onValueChange={setMigrateDest} options={destOptions} placeholder="Select destination" />
            </Field>
            <Button type="submit" disabled={!migrateDest}>Start migrate</Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
