import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { formatBytes, relativeTime } from "@/lib/utils";
import { toast } from "sonner";

type BackupRow = { id: string; path: string; size_bytes?: number; status?: string; created_at?: string; verified?: boolean };
type Preview = {
  id: string;
  path: string;
  size_bytes: number;
  checksum?: string;
  status?: string;
  created_at?: string;
  verified?: boolean;
  readable?: boolean;
  empty?: boolean;
  tables?: string[];
  statements?: number;
  warnings?: string[];
  restore_kind?: string;
  header?: string;
};

export function AdminBackupPreview() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["admin-backups"], queryFn: () => api.get<BackupRow[]>("/api/v1/admin/backups") });
  const [selected, setSelected] = useState<string>("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [busy, setBusy] = useState(false);
  async function loadPreview(id: string) {
    setSelected(id);
    try {
      const p = await api.get<Preview>(`/api/v1/admin/backups/${id}/preview`);
      setPreview(p);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Preview failed");
    }
  }
  return (
    <div>
      <PageHeader title="Backup preview" description="Inspect a logical dump before restore. Media libraries on disk are not included." />
      <ul className="mb-4 space-y-2">
        {(list.data || []).map((b) => (
          <li key={b.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border px-4 py-3">
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{b.path}</div>
              <div className="text-xs text-subtle">{relativeTime(b.created_at)} · {b.status}</div>
            </div>
            <Button size="sm" variant={selected === b.id ? "secondary" : "ghost"} onClick={() => loadPreview(b.id)}>Preview</Button>
          </li>
        ))}
      </ul>
      {preview && (
        <div className="space-y-3 rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex flex-wrap gap-2">
            <Badge tone={preview.verified ? "success" : "warning"}>{preview.verified ? "Verified" : "Unverified"}</Badge>
            <Badge>{preview.restore_kind || "unknown"}</Badge>
            <Badge>{formatBytes(preview.size_bytes)}</Badge>
            {preview.empty && <Badge tone="danger">Empty</Badge>}
          </div>
          {preview.header && <pre className="max-h-28 overflow-auto rounded-md bg-surface-2 p-3 font-mono text-[11px] text-muted">{preview.header}</pre>}
          <div className="text-sm text-muted">{preview.statements || 0} statements · {(preview.tables || []).length} tables in preview window</div>
          {(preview.tables || []).length > 0 && (
            <ul className="columns-2 text-sm md:columns-3">{preview.tables!.map((t) => <li key={t}>{t}</li>)}</ul>
          )}
          {(preview.warnings || []).map((w) => <p key={w} className="text-sm text-destructive">{w}</p>)}
          <Button disabled={busy || preview.empty || preview.restore_kind !== "sql" || (preview.warnings || []).some((w) => w.includes("incomplete"))} onClick={async () => {
            if (!window.confirm("Restore this dump into the live database? This cannot be undone.")) return;
            setBusy(true);
            try {
              await api.post(`/api/v1/admin/backups/${preview.id}/restore`, { confirm: true });
              toast.success("Restore completed");
              qc.invalidateQueries({ queryKey: ["admin-backups"] });
            } catch (e) {
              toast.error(e instanceof Error ? e.message : "Restore failed");
            } finally {
              setBusy(false);
            }
          }}>Restore this backup</Button>
        </div>
      )}
    </div>
  );
}
