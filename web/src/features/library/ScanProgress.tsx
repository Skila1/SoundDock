import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Progress } from "@/components/ui/misc";

export type ScanRun = {
  id: string;
  library_id: string;
  job_id?: string | null;
  kind: string;
  files_seen: number;
  files_added: number;
  files_failed: number;
  files_total: number;
  started_at: string;
  finished_at?: string | null;
  status: string;
  progress: number;
  last_error?: string | null;
};

export function useScanRuns(enabled = true) {
  return useQuery({
    queryKey: ["admin-scans"],
    queryFn: () => api.get<ScanRun[] | null>("/api/v1/admin/scans"),
    enabled,
    refetchInterval: 2000
  });
}

export function latestScan(runs: ScanRun[] | null | undefined, libraryId: string) {
  return (runs || []).find((s) => s.library_id === libraryId);
}

export function scanActive(s?: ScanRun | null) {
  if (!s) return false;
  if (s.finished_at) return false;
  const st = (s.status || "").toLowerCase();
  return st === "queued" || st === "running" || st === "retry" || st === "";
}

export function ScanProgressBar({ scan }: { scan?: ScanRun | null }) {
  if (!scan) return null;
  const active = scanActive(scan);
  const pct = Number(scan.progress) || 0;
  const seen = Number(scan.files_seen) || 0;
  const total = Number(scan.files_total) || 0;
  const failed = Number(scan.files_failed) || 0;
  const err = scan.last_error;
  const failedStatus = (scan.status || "").toLowerCase() === "failed";
  const label =
    failedStatus || err
      ? err || "Scan failed"
      : active
        ? total
          ? `Scanning ${seen} / ${total}`
          : "Listing files…"
        : failed
          ? `Scan finished · ${seen} files · ${failed} failed`
          : `Scan finished · ${seen} files`;
  return (
    <div className="mt-3 w-full min-w-[12rem] space-y-1">
      <Progress value={failedStatus ? pct : active ? Math.max(pct, 1) : 100} />
      <div className={`text-xs ${failedStatus || err ? "text-destructive" : "text-muted"}`}>{label}</div>
    </div>
  );
}
