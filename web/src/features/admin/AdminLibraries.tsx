import { useEffect, useState } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRightLeft, Combine, KeyRound, Pencil, Star, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge, Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { formatBytes } from "@/lib/utils";
import { ScanProgressBar, latestScan, useScanRuns } from "@/features/library/ScanProgress";
import { ByteInput, RowMenu, errorMessage } from "./adminUi";
import { capLabel } from "./adminFormat";
import { fetchQuotas, overrideFor, quotasKey, saveQuotas } from "./adminQuotas";

type Library = {
  id: string;
  name: string;
  kind: string;
  organisation_mode: string;
  storage_type?: string;
  track_count?: number;
  is_default?: boolean;
};

type StorageRow = {
  id: string;
  name: string;
  type: string;
  type_label?: string;
  description?: string;
  root?: string;
  endpoint?: string;
  bucket?: string;
  prefix?: string;
  region?: string;
  use_ssl?: boolean;
  used_bytes?: number;
  free_bytes?: number;
  total_bytes?: number;
  file_count?: number;
  can_delete?: boolean;
  libraries?: { id: string; name: string }[];
};

export function AdminLibraries() {
  const [params] = useSearchParams();
  if (params.get("tab") === "storage") return <Navigate to="/admin/storage" replace />;
  if (params.get("tab") === "access") return <Navigate to={`/admin/access${params.get("lib") ? `?lib=${params.get("lib")}` : ""}`} replace />;
  return (
    <div>
      <PageHeader title="Libraries" description="Your music libraries. Scan them from the Scan page and set who can use them under IAM > Access. Removing a library never deletes files on network or local folders." />
      <LibrariesTab />
    </div>
  );
}

export { AdminStorage } from "./AdminStorage";

function LibrariesTab() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const quotas = useQuery({ queryKey: quotasKey, queryFn: fetchQuotas });
  const scans = useScanRuns();
  const [createOpen, setCreateOpen] = useState(false);
  const [edit, setEdit] = useState<Library | null>(null);
  const [remove, setRemove] = useState<Library | null>(null);
  const [merge, setMerge] = useState<Library | null>(null);
  const [migrateFrom, setMigrateFrom] = useState<Library | null>(null);
  const list = libs.data || [];
  const defaultLib = list.find((l) => l.is_default) || list[0];
  const refresh = () => qc.invalidateQueries({ queryKey: ["libraries"] });
  const capFor = (id: string) => overrideFor(quotas.data?.libraries, (r) => r.library_id === id) ?? (quotas.data?.default_library_bytes || 0);

  return (
    <div>
      <div className="mb-3 flex justify-end">
        <Button onClick={() => setCreateOpen(true)}>Create library</Button>
      </div>
      <div className="space-y-3">
        {list.map((l) => {
          const scan = latestScan(scans.data, l.id);
          const used = quotas.data?.library_usage?.[l.id] || 0;
          const cap = capFor(l.id);
          return (
            <div key={l.id} className="rounded-2xl border border-border bg-surface-1 p-5 shadow-sm">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2 font-semibold">
                    {l.name}
                    {l.is_default && <Badge tone="success">Default</Badge>}
                  </div>
                  <div className="text-xs text-muted">
                    {l.track_count ?? 0} tracks · {formatBytes(used)}{cap ? ` of ${capLabel(cap)}` : ""} · {l.storage_type || "storage"} · {l.organisation_mode}
                  </div>
                </div>
                <div className="flex items-center gap-1">
                  <RowMenu
                    label={`Actions for ${l.name}`}
                    actions={[
                      { label: "Edit", icon: Pencil, onSelect: () => setEdit(l) },
                      { label: "Manage access", icon: KeyRound, onSelect: () => navigate(`/admin/access?lib=${l.id}`) },
                      ...(!l.is_default
                        ? [
                            {
                              label: "Make default",
                              icon: Star,
                              onSelect: async () => {
                                try {
                                  await api.post(`/api/v1/admin/libraries/${l.id}/default`);
                                  toast.success("Default library updated");
                                  refresh();
                                } catch (e) {
                                  toast.error(errorMessage(e, "Could not change default"));
                                }
                              }
                            },
                            { label: "Merge into default", icon: Combine, onSelect: () => setMerge(l) }
                          ]
                        : []),
                      { label: "Move files to another library", icon: ArrowRightLeft, onSelect: () => setMigrateFrom(l) },
                      { label: "Delete", icon: Trash2, destructive: true, separatorBefore: true, onSelect: () => setRemove(l) }
                    ]}
                  />
                </div>
              </div>
              {cap > 0 && <div className="mt-2"><Progress value={(used / cap) * 100} /></div>}
              <ScanProgressBar scan={scan} />
            </div>
          );
        })}
        {!list.length && <p className="text-sm text-muted">{libs.isLoading ? "Loading…" : "No libraries yet."}</p>}
      </div>

      <CreateLibraryDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={refresh} />
      <EditLibraryDialog lib={edit} override={edit ? overrideFor(quotas.data?.libraries, (r) => r.library_id === edit.id) : null} onClose={() => setEdit(null)} onSaved={() => { refresh(); qc.invalidateQueries({ queryKey: quotasKey }); }} />
      <MigrateDialog from={migrateFrom} libraries={list} onClose={() => setMigrateFrom(null)} onStarted={refresh} />
      <DeleteLibraryDialog lib={remove} onClose={() => setRemove(null)} onDeleted={refresh} />
      <ConfirmDialog
        open={!!merge}
        onOpenChange={(v) => { if (!v) setMerge(null); }}
        title={`Merge ${merge?.name || ""} into ${defaultLib?.name || "the default library"}?`}
        description="Tracks, albums, and history move into the default library without re-importing. The merged library is removed afterwards."
        confirmLabel="Merge"
        onConfirm={async () => {
          if (!merge || !defaultLib) return;
          try {
            await api.post(`/api/v1/admin/libraries/${defaultLib.id}/merge`, { source_ids: [merge.id] });
            toast.success("Merge queued");
            refresh();
          } catch (e) {
            toast.error(errorMessage(e, "Could not merge"));
          }
        }}
      />
    </div>
  );
}

function CreateLibraryDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (v: boolean) => void; onCreated: () => void }) {
  const storage = useQuery({ queryKey: ["admin-storage"], queryFn: () => api.get<StorageRow[]>("/api/v1/admin/storage"), enabled: open });
  const [form, setForm] = useState({ name: "", storage_id: "", kind: "music", organisation_mode: "virtual", prefix: "", read_only: false });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Create library">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await api.post("/api/v1/admin/libraries", { ...form });
              toast.success("Library created");
              onOpenChange(false);
              setForm({ ...form, name: "", prefix: "" });
              onCreated();
            } catch (err) {
              toast.error(errorMessage(err, "Could not create library"));
            }
          }}
        >
          <Field label="Name"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required /></Field>
          <Field label="Storage"><Select value={form.storage_id} onValueChange={(storage_id) => setForm({ ...form, storage_id })} placeholder="Choose storage" options={(storage.data || []).map((s) => ({ value: s.id, label: s.name }))} /></Field>
          <Field label="Folder inside the storage (optional)"><Input value={form.prefix} onChange={(e) => setForm({ ...form, prefix: e.target.value })} placeholder="music/" /></Field>
          <label className="flex items-center justify-between gap-3 text-sm">
            <span>Read only <span className="block text-xs text-subtle">Recommended for network and S3 folders you manage elsewhere.</span></span>
            <Switch checked={form.read_only} onCheckedChange={(read_only) => setForm({ ...form, read_only })} />
          </label>
          <Button type="submit" disabled={!form.storage_id}>Create</Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function EditLibraryDialog({ lib, override, onClose, onSaved }: { lib: Library | null; override: number | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState("");
  const [mode, setMode] = useState<"default" | "custom">("default");
  const [limit, setLimit] = useState<number | null>(null);
  useEffect(() => {
    if (!lib) return;
    setName(lib.name);
    setMode(override == null ? "default" : "custom");
    setLimit(override);
  }, [lib, override]);
  return (
    <Dialog open={!!lib} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title="Edit library">
        {lib && (
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              try {
                if (name !== lib.name) await api.patch(`/api/v1/admin/libraries/${lib.id}`, { name });
                const next = mode === "default" ? null : limit ?? 0;
                if (next !== override) await saveQuotas({ library: { id: lib.id, bytes: next } });
                toast.success("Library saved");
                onClose();
                onSaved();
              } catch (err) {
                toast.error(errorMessage(err, "Could not save library"));
              }
            }}
          >
            <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} required /></Field>
            <div>
              <div className="mb-1 text-sm font-medium">Storage limit</div>
              <div className="flex flex-wrap items-center gap-2">
                <Select className="w-44" value={mode} onValueChange={(v) => setMode(v as "default" | "custom")} options={[{ value: "default", label: "Use default" }, { value: "custom", label: "Custom limit" }]} />
                {mode === "custom" && <ByteInput value={limit} onChange={setLimit} placeholder="0 = unlimited" />}
              </div>
            </div>
            <Button type="submit">Save</Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function MigrateDialog({ from, libraries, onClose, onStarted }: { from: Library | null; libraries: Library[]; onClose: () => void; onStarted: () => void }) {
  const [dest, setDest] = useState("");
  useEffect(() => {
    if (!from) return;
    const others = libraries.filter((x) => x.id !== from.id);
    setDest((others.find((x) => x.storage_type === "managed") || others.find((x) => x.is_default) || others[0])?.id || "");
  }, [from, libraries]);
  return (
    <Dialog open={!!from} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title={from ? `Move files from ${from.name}` : "Move files"}>
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!from || !dest) return;
            try {
              const res = await api.post<{ requested_mode?: string; effective_mode?: string }>(`/api/v1/admin/libraries/${from.id}/migrate`, { dest_library_id: dest, mode: "move" });
              toast.success(res?.effective_mode === "copy" ? "Copy started (the source is not managed storage)" : "Move started");
              onClose();
              onStarted();
            } catch (err) {
              toast.error(errorMessage(err, "Could not start"));
            }
          }}
        >
          <p className="text-sm text-muted">Files in SoundDock-managed storage are moved. Files on network or S3 storage are copied and the originals are left alone.</p>
          <Field label="Destination library">
            <Select
              value={dest}
              onValueChange={setDest}
              placeholder="Choose a library"
              options={libraries.filter((x) => x.id !== from?.id).map((x) => ({ value: x.id, label: `${x.name}${x.storage_type === "managed" ? " (managed)" : ""}${x.is_default ? " · default" : ""}` }))}
            />
          </Field>
          <Button type="submit" disabled={!dest}>Start</Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DeleteLibraryDialog({ lib, onClose, onDeleted }: { lib: Library | null; onClose: () => void; onDeleted: () => void }) {
  const [deleteFiles, setDeleteFiles] = useState(false);
  useEffect(() => { setDeleteFiles(false); }, [lib]);
  return (
    <Dialog open={!!lib} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title={lib ? `Delete ${lib.name}?` : "Delete library"}>
        {lib && (
          <div className="space-y-3">
            <p className="text-sm text-muted">The library is removed from SoundDock. Files on network, local, or S3 storage are not deleted.</p>
            {lib.storage_type === "managed" && (
              <label className="flex items-center justify-between gap-3 text-sm">
                Also delete files SoundDock downloaded or stored for this library
                <Switch checked={deleteFiles} onCheckedChange={setDeleteFiles} />
              </label>
            )}
            <div className="flex gap-2">
              <Button
                variant="destructive"
                onClick={async () => {
                  try {
                    await api.del(`/api/v1/admin/libraries/${lib.id}`, { delete_files: deleteFiles });
                    toast.success("Library removed");
                    onClose();
                    onDeleted();
                  } catch (err) {
                    toast.error(errorMessage(err, "Could not delete library"));
                  }
                }}
              >
                Delete
              </Button>
              <Button variant="ghost" onClick={onClose}>Cancel</Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
