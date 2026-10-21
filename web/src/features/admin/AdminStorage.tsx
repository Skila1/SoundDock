import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Cloud, Folder, HardDrive, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge, Skeleton } from "@/components/ui/misc";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { formatBytes } from "@/lib/utils";
import { ByteInput, Card, RowMenu, SaveBar, errorMessage } from "./adminUi";
import { fetchQuotas, quotasKey, saveQuotas } from "./adminQuotas";
import { UsageBar } from "./storage/storageUi";

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
  health?: string;
  health_error?: string;
  libraries?: { id: string; name: string }[];
};

const storageKey = ["admin-storage"];
const emptyForm = { name: "", type: "local", root: "", endpoint: "", bucket: "", region: "auto", access_key: "", secret_key: "", prefix: "", use_ssl: true };
const typeIcon = { managed: HardDrive, local: Folder, s3: Cloud } as Record<string, typeof HardDrive>;

/** Where music files live, how full each place is, and the default size limits. */
export function AdminStorage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: storageKey, queryFn: () => api.get<StorageRow[]>("/api/v1/admin/storage") });
  const [open, setOpen] = useState(false);
  const [edit, setEdit] = useState<StorageRow | null>(null);
  const [remove, setRemove] = useState<StorageRow | null>(null);
  const [form, setForm] = useState(emptyForm);

  const saveConfig = () =>
    form.type === "s3"
      ? { endpoint: form.endpoint, bucket: form.bucket, region: form.region, access_key: form.access_key, secret_key: form.secret_key, prefix: form.prefix, use_ssl: form.use_ssl }
      : { root: form.root };
  const closeForm = () => {
    setOpen(false);
    setEdit(null);
  };

  return (
    <div className="space-y-5">
      <PageHeader
        title="Storage"
        description="Where your music files live, how full each place is, and the default size limits."
        actions={<Button onClick={() => { setForm(emptyForm); setOpen(true); }}><Plus /> Add storage</Button>}
      />

      {q.isError ? (
        <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />
      ) : q.isLoading ? (
        <div className="space-y-3">{Array.from({ length: 2 }).map((_, i) => <Skeleton key={i} className="h-32 w-full rounded-2xl" />)}</div>
      ) : !(q.data || []).length ? (
        <EmptyState icon={HardDrive} title="No storage yet." />
      ) : (
        <div className="space-y-3">
          {(q.data || []).map((s) => (
            <StorageCard
              key={s.id}
              s={s}
              onEdit={() => {
                setEdit(s);
                setForm({ ...emptyForm, name: s.name, type: s.type, root: s.root || "", endpoint: s.endpoint || "", bucket: s.bucket || "", region: s.region || "auto", prefix: s.prefix || "", use_ssl: s.use_ssl !== false });
              }}
              onRemove={() => setRemove(s)}
            />
          ))}
        </div>
      )}

      <LimitsCard />

      <Dialog open={open || !!edit} onOpenChange={(v) => { if (!v) closeForm(); }}>
        <DialogContent title={edit ? `Edit ${edit.name}` : "Add storage"} className="max-h-[90vh] overflow-y-auto">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              try {
                if (edit) {
                  await api.patch(`/api/v1/admin/storage/${edit.id}`, { name: form.name, config: saveConfig() });
                  toast.success("Storage updated");
                } else {
                  await api.post("/api/v1/admin/storage", { name: form.name, type: form.type, config: saveConfig() });
                  toast.success("Storage added");
                }
                closeForm();
                void qc.invalidateQueries({ queryKey: storageKey });
              } catch (err) {
                toast.error(errorMessage(err, "Could not save storage"));
              }
            }}
          >
            <Field label="Name"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required /></Field>
            {!edit && (
              <Field label="Type">
                <Select value={form.type} onValueChange={(type) => setForm({ ...form, type })} options={[{ value: "managed", label: "SoundDock media folder" }, { value: "local", label: "Folder on this server" }, { value: "s3", label: "S3 or Cloudflare R2" }]} />
              </Field>
            )}
            {form.type !== "s3" ? (
              <Field label="Folder path" hint="Leave empty to use SoundDock's own media folder.">
                <Input value={form.root} onChange={(e) => setForm({ ...form, root: e.target.value })} placeholder="/srv/music" />
              </Field>
            ) : (
              <>
                <Field label="Endpoint"><Input value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} placeholder="xxx.r2.cloudflarestorage.com" required={!edit} /></Field>
                <Field label="Bucket"><Input value={form.bucket} onChange={(e) => setForm({ ...form, bucket: e.target.value })} required={!edit} /></Field>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="Region"><Input value={form.region} onChange={(e) => setForm({ ...form, region: e.target.value })} /></Field>
                  <Field label="Folder in bucket"><Input value={form.prefix} onChange={(e) => setForm({ ...form, prefix: e.target.value })} /></Field>
                </div>
                <Field label="Access key" hint={edit ? "Leave blank to keep the saved key." : undefined}><Input value={form.access_key} onChange={(e) => setForm({ ...form, access_key: e.target.value })} /></Field>
                <Field label="Secret key" hint="Stored encrypted and never shown again."><Input type="password" value={form.secret_key} onChange={(e) => setForm({ ...form, secret_key: e.target.value })} /></Field>
              </>
            )}
            <div className="flex gap-2">
              <Button type="submit">Save</Button>
              <Button type="button" variant="ghost" onClick={closeForm}>Cancel</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={!!remove}
        onOpenChange={(v) => { if (!v) setRemove(null); }}
        title={`Remove ${remove?.name || "storage"}?`}
        description="SoundDock stops using this storage. Files in it are not deleted."
        confirmLabel="Remove"
        destructive
        onConfirm={async () => {
          if (!remove) return;
          try {
            await api.del(`/api/v1/admin/storage/${remove.id}`);
            toast.success("Storage removed");
            void qc.invalidateQueries({ queryKey: storageKey });
          } catch (e) {
            toast.error(errorMessage(e, "Could not remove storage"));
          }
        }}
      />
    </div>
  );
}

function StorageCard({ s, onEdit, onRemove }: { s: StorageRow; onEdit: () => void; onRemove: () => void }) {
  const Icon = typeIcon[s.type] || HardDrive;
  const used = s.used_bytes || 0;
  const total = s.total_bytes || 0;
  const free = s.free_bytes || 0;
  const diskUsed = total > 0 ? Math.max(used, total - free) : 0;
  const other = Math.max(0, diskUsed - used);
  const where = s.root || (s.bucket ? `${s.bucket}${s.prefix ? `/${s.prefix}` : ""}` : "");
  const libs = s.libraries || [];

  return (
    <article className="rounded-2xl border border-border bg-surface-1 p-5 shadow-sm">
      <div className="flex items-start gap-3">
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-accent/10 text-accent">
          <Icon className="h-5 w-5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold">{s.name}</h3>
            <Badge>{s.type_label || s.type}</Badge>
            {s.health === "fail" && <Badge tone="danger">Unreachable</Badge>}
          </div>
          {where && <p className="mt-0.5 truncate font-mono text-xs text-subtle" title={where}>{where}{s.bucket && s.endpoint ? ` · ${s.endpoint}` : ""}</p>}
        </div>
        <RowMenu
          label={`Actions for ${s.name}`}
          actions={[
            { label: "Edit", icon: Pencil, onSelect: onEdit },
            { label: s.can_delete ? "Remove" : "Remove (a library still uses it)", icon: Trash2, destructive: true, disabled: !s.can_delete, separatorBefore: true, onSelect: onRemove }
          ]}
        />
      </div>

      <div className="mt-4">
        {total > 0 ? (
          <>
            <div className="relative">
              <UsageBar used={diskUsed} total={total} />
              {/* SoundDock's own share of the disk, drawn over the full used bar. */}
              {used > 0 && <div className="pointer-events-none absolute inset-y-0 left-0 rounded-full bg-accent" style={{ width: `${Math.min(100, (used / total) * 100)}%` }} />}
            </div>
            <div className="mt-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs text-muted">
              <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-full bg-accent" />SoundDock {formatBytes(used)}</span>
                {other > 0 && <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-full bg-surface-3 ring-1 ring-border" />Other {formatBytes(other)}</span>}
                {typeof s.file_count === "number" && <span>{s.file_count.toLocaleString()} files</span>}
              </span>
              <span className="tabular"><span className="font-medium text-foreground">{formatBytes(free)}</span> free of {formatBytes(total)}</span>
            </div>
          </>
        ) : (
          <p className="text-xs text-muted">
            {s.health === "fail" ? s.health_error || "Cannot reach this storage." : `${formatBytes(used)} used${typeof s.file_count === "number" ? ` · ${s.file_count.toLocaleString()} files` : ""}. Free space is not reported for this storage.`}
          </p>
        )}
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-1.5 text-xs">
        <span className="text-subtle">{libs.length ? "Libraries:" : "No libraries use this yet."}</span>
        {libs.map((l) => (
          <Link key={l.id} to="/admin/libraries" className="rounded-full bg-surface-2 px-2 py-0.5 font-medium text-muted hover:text-foreground">
            {l.name}
          </Link>
        ))}
      </div>
    </article>
  );
}

function LimitsCard() {
  const qc = useQueryClient();
  const quotas = useQuery({ queryKey: quotasKey, queryFn: fetchQuotas });
  const [lib, setLib] = useState<number | null>(null);
  const [user, setUser] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const savedLib = quotas.data?.default_library_bytes || 0;
  const savedUser = quotas.data?.default_user_bytes || 0;
  useEffect(() => {
    if (!quotas.data) return;
    setLib(quotas.data.default_library_bytes || 0);
    setUser(quotas.data.default_user_bytes || 0);
  }, [quotas.data]);
  const dirty = !!quotas.data && ((lib || 0) !== savedLib || (user || 0) !== savedUser);

  const save = async () => {
    setSaving(true);
    try {
      await saveQuotas({ defaultLibraryBytes: lib || 0, defaultUserBytes: user || 0 });
      toast.success("Limits saved");
      void qc.invalidateQueries({ queryKey: quotasKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save limits"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card title="Limits" description="Defaults for everyone. A library or person with its own limit keeps it. 0 means unlimited.">
      {!quotas.data ? (
        <Skeleton className="h-20 w-full" />
      ) : (
        <div className="divide-y divide-border">
          <LimitRow label="Each library can hold" hint="Change one library's limit from its Edit menu on the Libraries page.">
            <ByteInput value={lib} onChange={setLib} placeholder="0" />
          </LimitRow>
          <LimitRow label="Each person can upload" hint="Change one person's limit on their user page.">
            <ByteInput value={user} onChange={setUser} placeholder="0" />
          </LimitRow>
        </div>
      )}
      <SaveBar dirty={dirty} saving={saving} onSave={() => void save()} onReset={() => { setLib(savedLib); setUser(savedUser); }} />
    </Card>
  );
}

function LimitRow({ label, hint, children }: { label: string; hint: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        <p className="text-xs text-subtle">{hint}</p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}
