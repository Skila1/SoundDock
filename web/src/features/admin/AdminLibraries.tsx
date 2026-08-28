import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { PageHeader } from "@/components/ui/empty";
import { Badge } from "@/components/ui/misc";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import { ScanProgressBar, latestScan, scanActive, useScanRuns } from "@/features/library/ScanProgress";

type Library = {
  id: string;
  name: string;
  kind: string;
  organisation_mode: string;
  storage_type?: string;
  track_count?: number;
  is_default?: boolean;
};

export function AdminLibraries() {
  const qc = useQueryClient();
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const storage = useQuery({ queryKey: ["admin-storage"], queryFn: () => api.get<any[]>("/api/v1/admin/storage") });
  const scans = useScanRuns();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: "", storage_id: "", kind: "music", organisation_mode: "virtual", prefix: "", read_only: false });
  const [edit, setEdit] = useState<Library | null>(null);
  const [rename, setRename] = useState("");
  const [remove, setRemove] = useState<Library | null>(null);
  const [deleteFiles, setDeleteFiles] = useState(false);
  const [mergeSrc, setMergeSrc] = useState<string[]>([]);
  const [migrateFrom, setMigrateFrom] = useState<Library | null>(null);
  const [migrateDest, setMigrateDest] = useState("");

  const list = libs.data || [];
  const defaultId = list.find((l) => l.is_default)?.id || list[0]?.id;

  return (
    <div>
      <PageHeader
        title="Libraries"
        description="One catalogue per library. Merge duplicates into the default library without reimporting. Removing a library never deletes NAS or local source files."
        actions={<Button onClick={() => setOpen(true)}>Create library</Button>}
      />
      {mergeSrc.length > 0 && defaultId && (
        <div className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-border bg-surface-1 px-3 py-2 text-sm">
          <span>{mergeSrc.length} selected to merge into the default library</span>
          <Button
            size="sm"
            onClick={async () => {
              await api.post(`/api/v1/admin/libraries/${defaultId}/merge`, { source_ids: mergeSrc });
              toast.success("Merge queued");
              setMergeSrc([]);
              qc.invalidateQueries({ queryKey: ["libraries"] });
            }}
          >
            Merge into default
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setMergeSrc([])}>Clear</Button>
        </div>
      )}
      <div className="space-y-3">
        {list.map((l) => {
          const scan = latestScan(scans.data, l.id);
          const busy = scanActive(scan);
          const managed = l.storage_type === "managed";
          return (
            <div key={l.id} className="rounded-xl border border-border bg-surface-1 p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <label className="flex min-w-0 items-start gap-3">
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={mergeSrc.includes(l.id)}
                    disabled={l.is_default}
                    onChange={() => {
                      setMergeSrc((prev) => prev.includes(l.id) ? prev.filter((id) => id !== l.id) : [...prev, l.id]);
                    }}
                  />
                  <div>
                    <div className="flex flex-wrap items-center gap-2 font-semibold">
                      {l.name}
                      {l.is_default && <Badge tone="success">Default</Badge>}
                    </div>
                    <div className="text-xs text-muted">{l.kind} · {l.organisation_mode} · {l.storage_type || "storage"} · {l.track_count ?? 0} tracks</div>
                  </div>
                </label>
                <div className="flex flex-wrap gap-2">
                  {!l.is_default && (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={async () => {
                        await api.post(`/api/v1/admin/libraries/${l.id}/default`);
                        toast.success("Default library updated");
                        qc.invalidateQueries({ queryKey: ["libraries"] });
                      }}
                    >
                      Set default
                    </Button>
                  )}
                  <Button size="sm" variant="secondary" onClick={() => { setEdit(l); setRename(l.name); }}>Rename</Button>
                  <Button
                    size="sm"
                    disabled={busy}
                    onClick={async () => {
                      await api.post(`/api/v1/admin/libraries/${l.id}/scan`);
                      toast.success("Scan started");
                      scans.refetch();
                    }}
                  >
                    {busy ? "Scanning…" : "Scan"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      const others = list.filter((x) => x.id !== l.id);
                      const preferred = others.find((x) => x.storage_type === "managed") || others.find((x) => x.is_default) || others[0];
                      setMigrateFrom(l);
                      setMigrateDest(preferred?.id || "");
                    }}
                  >
                    Migrate
                  </Button>
                  <Button size="sm" variant="destructive" onClick={() => { setRemove(l); setDeleteFiles(false); }}>Delete</Button>
                </div>
              </div>
              <ScanProgressBar scan={scan} />
              {remove?.id === l.id && (
                <div className="mt-3 space-y-2 rounded-lg border border-border p-3">
                  <p className="text-sm text-muted">
                    This removes the library from SoundDock. Source media on NAS, local, or external storage is not deleted.
                  </p>
                  {managed && (
                    <label className="flex items-center justify-between gap-3 text-sm">
                      Also delete SoundDock-managed files
                      <Switch checked={deleteFiles} onCheckedChange={setDeleteFiles} />
                    </label>
                  )}
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={async () => {
                        await api.del(`/api/v1/admin/libraries/${l.id}`, { delete_files: deleteFiles });
                        toast.success("Library removed");
                        setRemove(null);
                        qc.invalidateQueries({ queryKey: ["libraries"] });
                      }}
                    >
                      Confirm delete
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setRemove(null)}>Cancel</Button>
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="Create library">
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            await api.post("/api/v1/admin/libraries", {
              name: form.name,
              kind: form.kind,
              storage_id: form.storage_id,
              organisation_mode: form.organisation_mode,
              prefix: form.prefix,
              read_only: form.read_only
            });
            toast.success("Library created");
            setOpen(false);
            qc.invalidateQueries({ queryKey: ["libraries"] });
          }}>
            <Field label="Name"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required /></Field>
            <Field label="Storage"><Select value={form.storage_id} onValueChange={(storage_id) => setForm({ ...form, storage_id })} options={(storage.data || []).map((s: any) => ({ value: s.id, label: s.name }))} /></Field>
            <Field label="Root prefix"><Input value={form.prefix} onChange={(e) => setForm({ ...form, prefix: e.target.value })} placeholder="optional path prefix" /></Field>
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm">Read only (default on for NAS/S3)</span>
              <Switch checked={form.read_only} onCheckedChange={(read_only) => setForm({ ...form, read_only })} />
            </div>
            <Button type="submit">Create</Button>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={!!migrateFrom} onOpenChange={(v) => { if (!v) setMigrateFrom(null); }}>
        <DialogContent title={migrateFrom ? `Migrate ${migrateFrom.name}` : "Migrate"}>
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            if (!migrateFrom) return;
            if (!migrateDest) {
              toast.error("Choose a destination library");
              return;
            }
            const res = await api.post<{ requested_mode?: string; effective_mode?: string; reason?: string }>(
              `/api/v1/admin/libraries/${migrateFrom.id}/migrate`,
              { dest_library_id: migrateDest, mode: "move" }
            );
            if (res?.requested_mode === "move" && res?.effective_mode === "copy") {
              toast.success("Migration started as copy (source is not managed storage)");
            } else if (res?.effective_mode === "move") {
              toast.success("Migration started as move after destination ingest");
            } else {
              toast.success("Migration started");
            }
            setMigrateFrom(null);
            qc.invalidateQueries({ queryKey: ["libraries"] });
          }}>
            <p className="text-sm text-muted">Move is used only when the source is managed storage and the destination ingest succeeds. NAS and S3 sources are copied; the API reports requested_mode, effective_mode, and reason.</p>
            <Field label="Destination library">
              <Select
                value={migrateDest}
                onValueChange={setMigrateDest}
                placeholder="Select destination"
                options={list.filter((x) => x.id !== migrateFrom?.id).map((x) => ({
                  value: x.id,
                  label: `${x.name}${x.storage_type === "managed" ? " (managed)" : ""}${x.is_default ? " · default" : ""}`
                }))}
              />
            </Field>
            <Button type="submit" disabled={!migrateDest}>Start migrate</Button>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={!!edit} onOpenChange={(v) => { if (!v) setEdit(null); }}>
        <DialogContent title="Rename library">
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            if (!edit) return;
            await api.patch(`/api/v1/admin/libraries/${edit.id}`, { name: rename });
            toast.success("Library renamed");
            setEdit(null);
            qc.invalidateQueries({ queryKey: ["libraries"] });
          }}>
            <Field label="Name"><Input value={rename} onChange={(e) => setRename(e.target.value)} required /></Field>
            <Button type="submit">Save</Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
