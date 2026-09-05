import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge, Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { formatBytes, relativeTime } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import type { User } from "@/types/api";
import { DiscordServerButton, HelpButton } from "@/components/community/CommunityLinks";

type AdminUserRow = {
  id: string;
  username: string;
  display_name?: string;
  email?: string | null;
  disabled: boolean;
  created_at: string;
  discord_id?: string | null;
  discord_username?: string | null;
  role?: string;
  roles?: string[];
};

export function AdminUsers() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const q = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<AdminUserRow[]>("/api/v1/admin/users") });
  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<AdminUserRow | null>(null);
  const [discordInspect, setDiscordInspect] = useState("");
  const [form, setForm] = useState({ username: "", password: "", role: "User" });
  const rows = (q.data || []).filter((u) => `${u.username} ${u.display_name || ""} ${u.email || ""} ${u.discord_id || ""} ${u.discord_username || ""} ${u.role || ""}`.toLowerCase().includes(filter.toLowerCase()));
  return (
    <div>
      <PageHeader title="Users" description="Click a user to change access, unlink Discord, or inspect their personal library." actions={<Button onClick={() => setOpen(true)}>Create user</Button>} />
      <Input className="mb-4 max-w-sm" placeholder="Search users" value={filter} onChange={(e) => setFilter(e.target.value)} />
      <form
        className="mb-4 flex max-w-xl flex-wrap items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const id = discordInspect.trim();
          if (id) location.href = `/admin/discord-users/${encodeURIComponent(id)}/library`;
        }}
      >
        <Field label="Inspect Discord-only library" hint="Snowflake ID. Use this for requesters who have not logged into the web app.">
          <Input value={discordInspect} onChange={(e) => setDiscordInspect(e.target.value)} placeholder="Discord user ID" />
        </Field>
        <Button type="submit" variant="secondary" disabled={!discordInspect.trim()}>Open</Button>
      </form>
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted"><tr><th className="p-3">User</th><th className="p-3">Discord</th><th className="p-3">Access</th><th className="p-3">Status</th><th className="p-3">Created</th></tr></thead>
          <tbody>
            {rows.map((u) => (
              <tr key={u.id} className="cursor-pointer border-t border-border hover:bg-surface-2/60" onClick={() => setSelected(u)}>
                <td className="p-3">
                  <div className="font-medium">{u.display_name || u.username}</div>
                  <div className="text-xs text-subtle">{u.username}{u.email ? ` · ${u.email}` : ""}</div>
                </td>
                <td className="p-3">
                  {u.discord_id ? u.discord_id : <span className="text-subtle">-</span>}
                </td>
                <td className="p-3"><Badge tone={u.role === "Administrator" ? "success" : "neutral"}>{u.role || "User"}</Badge></td>
                <td className="p-3"><Badge tone={u.disabled ? "danger" : "success"}>{u.disabled ? "Disabled" : "Active"}</Badge></td>
                <td className="p-3 text-muted">{relativeTime(u.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ManageUserDialog
        user={selected}
        selfId={me.data?.id}
        onClose={() => setSelected(null)}
        onChanged={async () => {
          await qc.invalidateQueries({ queryKey: ["admin-users"] });
        }}
        onDeleted={async () => {
          setSelected(null);
          await qc.invalidateQueries({ queryKey: ["admin-users"] });
        }}
      />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="Create user">
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            await api.post("/api/v1/admin/users", form);
            toast.success("User created");
            setOpen(false);
            qc.invalidateQueries({ queryKey: ["admin-users"] });
          }}>
            <Field label="Username"><Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required /></Field>
            <Field label="Password"><Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} required /></Field>
            <Field label="Role"><Select value={form.role} onValueChange={(role) => setForm({ ...form, role })} options={[{ value: "User", label: "User" }, { value: "Administrator", label: "Administrator" }]} /></Field>
            <Button type="submit">Create</Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ManageUserDialog({
  user,
  selfId,
  onClose,
  onChanged,
  onDeleted
}: {
  user: AdminUserRow | null;
  selfId?: string;
  onClose: () => void;
  onChanged: () => Promise<void>;
  onDeleted: () => Promise<void>;
}) {
  const [role, setRole] = useState("User");
  const [disabled, setDisabled] = useState(false);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  useEffect(() => {
    if (!user) {
      setConfirmDelete(false);
      return;
    }
    setRole(user.role || "User");
    setDisabled(!!user.disabled);
    setConfirmDelete(false);
  }, [user]);
  const isSelf = !!user && user.id === selfId;
  async function saveAccess() {
    if (!user) return;
    setBusy(true);
    try {
      await api.patch(`/api/v1/admin/users/${user.id}`, { role, disabled });
      toast.success("Access saved");
      await onChanged();
      onClose();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save access");
    } finally {
      setBusy(false);
    }
  }
  async function unlinkDiscord() {
    if (!user) return;
    setBusy(true);
    try {
      await api.del(`/api/v1/admin/users/${user.id}/identities/discord`);
      toast.success("Discord unlinked");
      await onChanged();
      onClose();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not unlink Discord");
    } finally {
      setBusy(false);
    }
  }
  async function wipeUser() {
    if (!user) return;
    setBusy(true);
    try {
      await api.del(`/api/v1/admin/users/${user.id}`);
      toast.success("User deleted");
      await onDeleted();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete user");
      setConfirmDelete(false);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open={!!user} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title={user ? (user.display_name || user.username) : "User"}>
        {user && (
          <div className="space-y-4">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              <dt className="text-muted">Username</dt><dd className="font-medium">{user.username}</dd>
              <dt className="text-muted">Display name</dt><dd>{user.display_name || "-"}</dd>
              <dt className="text-muted">Email</dt><dd>{user.email || "-"}</dd>
              <dt className="text-muted">Discord</dt>
              <dd>{user.discord_id || "Not linked"}</dd>
              <dt className="text-muted">Created</dt><dd>{relativeTime(user.created_at)}</dd>
            </dl>
            <Field label="Role" hint="Updates User vs Administrator only. Custom groups stay assigned.">
              <Select value={role} onValueChange={setRole} options={[{ value: "User", label: "User" }, { value: "Administrator", label: "Administrator" }]} />
            </Field>
            <div className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2">
              <div>
                <div className="text-sm font-medium">Disabled</div>
                <p className="text-xs text-subtle">{isSelf ? "You cannot disable your own account." : "Blocked from signing in. Sessions are ended."}</p>
              </div>
              <Switch checked={disabled} onCheckedChange={(v) => { if (!isSelf) setDisabled(v); }} />
            </div>
            <div className="flex flex-wrap gap-2">
              <Button type="button" disabled={busy} onClick={saveAccess}>Save access</Button>
              <Button type="button" variant="secondary" onClick={() => { location.href = `/admin/users/${user.id}/library`; }}>
                Open personal library
              </Button>
              {user.discord_id && (
                <Button type="button" variant="secondary" disabled={busy} onClick={unlinkDiscord}>Unlink Discord</Button>
              )}
            </div>
            <div className="border-t border-border pt-4">
              {isSelf ? (
                <p className="text-xs text-subtle">You cannot delete your own account.</p>
              ) : confirmDelete ? (
                <div className="space-y-2">
                  <p className="text-sm text-muted">This wipes the account, sessions, playlists, and Discord link. It cannot be undone.</p>
                  <div className="flex gap-2">
                    <Button type="button" variant="destructive" disabled={busy} onClick={wipeUser}>Delete forever</Button>
                    <Button type="button" variant="ghost" disabled={busy} onClick={() => setConfirmDelete(false)}>Cancel</Button>
                  </div>
                </div>
              ) : (
                <Button type="button" variant="destructive" disabled={busy} onClick={() => setConfirmDelete(true)}>Delete user</Button>
              )}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

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
  lib_count?: number;
  can_delete?: boolean;
  libraries?: { id: string; name: string }[];
};

const emptyStorageForm = {
  name: "", type: "local", root: "",
  endpoint: "", bucket: "", region: "auto", access_key: "", secret_key: "", prefix: "", use_ssl: true
};

export function AdminStorage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-storage"], queryFn: () => api.get<StorageRow[]>("/api/v1/admin/storage") });
  const [open, setOpen] = useState(false);
  const [edit, setEdit] = useState<StorageRow | null>(null);
  const [form, setForm] = useState(emptyStorageForm);

  const saveConfig = () => {
    if (form.type === "s3") {
      return {
        endpoint: form.endpoint, bucket: form.bucket, region: form.region,
        access_key: form.access_key, secret_key: form.secret_key, prefix: form.prefix, use_ssl: form.use_ssl
      };
    }
    return { root: form.root };
  };

  return (
    <div>
      <PageHeader title="Storage" description="Where SoundDock keeps media. Managed is the local folder this instance writes downloads into." actions={<Button onClick={() => { setForm(emptyStorageForm); setOpen(true); }}>Add provider</Button>} />
      <div className="grid gap-3 md:grid-cols-2">
        {(q.data || []).map((s) => {
          const used = s.used_bytes || 0;
          const total = s.total_bytes || 0;
          const free = s.free_bytes || 0;
          const pct = total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
          return (
            <article key={s.id} className="rounded-xl border border-border bg-surface-1 p-4">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <h3 className="font-semibold">{s.name}</h3>
                  <p className="text-sm text-muted">{s.type_label || s.type}</p>
                </div>
                <Badge tone="success">Configured</Badge>
              </div>
              <p className="mt-2 text-sm text-subtle">{s.description}</p>
              {s.root && <p className="mt-2 break-all font-mono text-xs text-muted">{s.root}</p>}
              {s.bucket && <p className="mt-2 text-xs text-muted">{s.bucket}{s.prefix ? ` / ${s.prefix}` : ""} · {s.endpoint || "S3"}</p>}
              {(total > 0 || used > 0) && (
                <div className="mt-3">
                  <div className="mb-1 flex justify-between text-xs text-muted">
                    <span>Used {formatBytes(used)}{typeof s.file_count === "number" ? ` · ${s.file_count} files` : ""}</span>
                    {total > 0 && <span>Free {formatBytes(free)}</span>}
                  </div>
                  {total > 0 && <Progress value={pct} />}
                  {total > 0 && <p className="mt-1 text-xs text-subtle">{formatBytes(used)} of {formatBytes(total)} on this volume</p>}
                </div>
              )}
              {(s.libraries || []).length > 0 && (
                <p className="mt-2 text-xs text-muted">Libraries: {(s.libraries || []).map((l) => l.name).join(", ")}</p>
              )}
              <div className="mt-3 flex flex-wrap gap-2">
                <Button size="sm" variant="secondary" onClick={() => {
                  setEdit(s);
                  setForm({
                    ...emptyStorageForm,
                    name: s.name,
                    type: s.type,
                    root: s.root || "",
                    endpoint: s.endpoint || "",
                    bucket: s.bucket || "",
                    region: s.region || "auto",
                    prefix: s.prefix || "",
                    use_ssl: s.use_ssl !== false
                  });
                }}>Edit</Button>
                <Button size="sm" variant="ghost" disabled={!s.can_delete} onClick={async () => {
                  if (!s.can_delete) return;
                  await api.del(`/api/v1/admin/storage/${s.id}`);
                  toast.success("Provider removed");
                  qc.invalidateQueries({ queryKey: ["admin-storage"] });
                }}>Delete</Button>
              </div>
            </article>
          );
        })}
      </div>
      <Dialog open={open || !!edit} onOpenChange={(v) => { if (!v) { setOpen(false); setEdit(null); } }}>
        <DialogContent title={edit ? `Edit ${edit.name}` : "Add storage"}>
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            if (edit) {
              await api.patch(`/api/v1/admin/storage/${edit.id}`, { name: form.name, config: saveConfig() });
              toast.success("Storage updated");
            } else {
              await api.post("/api/v1/admin/storage", { name: form.name, type: form.type, config: saveConfig() });
              toast.success("Storage added");
            }
            setOpen(false);
            setEdit(null);
            qc.invalidateQueries({ queryKey: ["admin-storage"] });
          }}>
            <Field label="Name"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required /></Field>
            {!edit && (
              <Field label="Type">
                <Select value={form.type} onValueChange={(type) => setForm({ ...form, type })} options={[
                  { value: "managed", label: "Managed (SoundDock media folder)" },
                  { value: "local", label: "Local folder" },
                  { value: "s3", label: "S3 / R2" }
                ]} />
              </Field>
            )}
            {form.type !== "s3" ? (
              <Field label="Folder path" hint="Leave empty to use the instance managed folder.">
                <Input value={form.root} onChange={(e) => setForm({ ...form, root: e.target.value })} placeholder="e.g. D:\\SoundDock\\data\\managed" />
              </Field>
            ) : (
              <>
                <Field label="Endpoint"><Input value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} placeholder="xxx.r2.cloudflarestorage.com" required={!edit} /></Field>
                <Field label="Bucket"><Input value={form.bucket} onChange={(e) => setForm({ ...form, bucket: e.target.value })} required={!edit} /></Field>
                <Field label="Region"><Input value={form.region} onChange={(e) => setForm({ ...form, region: e.target.value })} /></Field>
                <Field label="Prefix"><Input value={form.prefix} onChange={(e) => setForm({ ...form, prefix: e.target.value })} /></Field>
                <Field label="Access key" hint="Leave blank when editing to keep the current key."><Input value={form.access_key} onChange={(e) => setForm({ ...form, access_key: e.target.value })} /></Field>
                <Field label="Secret key" hint="Write-only. Never shown again."><Input type="password" value={form.secret_key} onChange={(e) => setForm({ ...form, secret_key: e.target.value })} /></Field>
              </>
            )}
            <Button type="submit">Save</Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

type BackupSettings = {
  local_enabled?: boolean;
  r2_enabled?: boolean;
  include_media?: boolean;
  scheduled_enabled?: boolean;
  endpoint?: string;
  region?: string;
  bucket?: string;
  access_key?: string;
  secret_key?: string;
  prefix?: string;
  use_ssl?: boolean;
  secret_set?: boolean;
  restore_passphrase_set?: boolean;
  reminder_pending?: boolean;
};

type RestoreRequirement = {
  key: string;
  class: string;
  source: string;
  present_at_backup?: boolean;
  present_on_host?: boolean;
  recovered?: boolean;
  note?: string;
};

export function AdminBackups() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-backups"], queryFn: () => api.get<any[]>("/api/v1/admin/backups") });
  const settings = useQuery({ queryKey: ["admin-backup-settings"], queryFn: () => api.get<BackupSettings>("/api/v1/admin/backups/settings") });
  const reqs = useQuery({ queryKey: ["admin-backup-requirements"], queryFn: () => api.get<{ items?: RestoreRequirement[]; instance_name?: string }>("/api/v1/admin/backups/restore-requirements") });
  const remote = useQuery({
    queryKey: ["admin-backup-remote"],
    queryFn: () => api.get<any[]>("/api/v1/admin/backups/remote"),
    enabled: !!settings.data?.r2_enabled
  });
  const [form, setForm] = useState<BackupSettings>({});
  const [busy, setBusy] = useState(false);
  const [passphrase, setPassphrase] = useState("");
  const [currentPass, setCurrentPass] = useState("");
  const [restorePass, setRestorePass] = useState("");
  const [restoreId, setRestoreId] = useState("");
  const st = { local_enabled: true, include_media: true, use_ssl: true, ...settings.data, ...form };
  const last = q.data?.[0];
  const passphraseSet = !!settings.data?.restore_passphrase_set;
  const reminderPending = !!settings.data?.reminder_pending;

  async function saveSettings() {
    await api.put("/api/v1/admin/backups/settings", st);
    toast.success("Backup settings saved");
    qc.invalidateQueries({ queryKey: ["admin-backup-settings"] });
    qc.invalidateQueries({ queryKey: ["admin-backup-remote"] });
  }

  async function downloadReminder() {
    const res = await fetch("/api/v1/admin/backups/reminder", { credentials: "include" });
    if (!res.ok) {
      toast.error("Reminder is not available");
      return;
    }
    const text = await res.text();
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "sounddock-recovery-reminder.txt";
    a.click();
    URL.revokeObjectURL(url);
    qc.invalidateQueries({ queryKey: ["admin-backup-settings"] });
  }

  return (
    <div>
      <PageHeader
        title="Backups"
        description="Encrypted instance backups include the database, managed media, artwork, and on-disk lyrics. NAS library trees are not packed. A recovery passphrase is required before any backup."
        actions={<Button disabled={busy || !passphraseSet} onClick={async () => {
          setBusy(true);
          try {
            await api.post("/api/v1/admin/backups");
            toast.success("Backup finished");
            qc.invalidateQueries({ queryKey: ["admin-backups"] });
            qc.invalidateQueries({ queryKey: ["admin-backup-remote"] });
          } catch (e: any) {
            toast.error(e?.message || "Backup failed");
          } finally {
            setBusy(false);
          }
        }}>Run backup</Button>}
      />
      {(reqs.data?.items || []).length > 0 && (
        <article className="mb-5 rounded-xl border border-border bg-surface-1 p-4">
          <h3 className="mb-2 font-semibold">Restore requirements</h3>
          <p className="mb-3 text-sm text-muted">Recovered values versus items this host still needs. NAS bind and public URL are never copied from the archive.</p>
          <ul className="mb-3 space-y-2 text-sm">
            {reqs.data!.items!.map((it) => (
              <li key={it.key} className="rounded-lg border border-border px-3 py-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{it.key}</span>
                  <Badge tone={it.recovered ? "success" : "warning"}>{it.class}</Badge>
                  {it.present_on_host ? <Badge>on this host</Badge> : null}
                </div>
                {it.note && <p className="mt-1 text-xs text-subtle">{it.note}</p>}
              </li>
            ))}
          </ul>
          <Button size="sm" variant="secondary" onClick={async () => {
            await api.post("/api/v1/admin/backups/restore-requirements/dismiss");
            qc.invalidateQueries({ queryKey: ["admin-backup-requirements"] });
          }}>Dismiss</Button>
        </article>
      )}
      <article className="mb-5 rounded-xl border border-border bg-surface-1 p-4">
        <h3 className="mb-2 font-semibold">Recovery passphrase</h3>
        <p className="mb-3 text-sm text-muted">
          SoundDock never stores this passphrase. Scheduled backups use a boxed archive key and do not prompt.
          {passphraseSet ? " Changing it re-wraps future backups. Old backups stay recoverable with the old passphrase." : " Set it before the first backup."}
        </p>
        <div className="mb-3 grid gap-3 md:grid-cols-2">
          {passphraseSet && (
            <Field label="Current passphrase">
              <Input type="password" value={currentPass} onChange={(e) => setCurrentPass(e.target.value)} autoComplete="off" />
            </Field>
          )}
          <Field label={passphraseSet ? "New passphrase" : "Passphrase"} hint="At least 12 characters.">
            <Input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} autoComplete="new-password" />
          </Field>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={async () => {
            try {
              await api.post("/api/v1/admin/backups/passphrase", {
                passphrase,
                current_passphrase: currentPass
              });
              setPassphrase("");
              setCurrentPass("");
              toast.success(passphraseSet ? "Passphrase updated. Old backups stay recoverable with the old passphrase." : "Recovery passphrase set");
              qc.invalidateQueries({ queryKey: ["admin-backup-settings"] });
            } catch (e: any) {
              toast.error(e?.message || "Could not set passphrase");
            }
          }}>{passphraseSet ? "Change passphrase" : "Set passphrase"}</Button>
          {reminderPending && (
            <>
              <Button size="sm" variant="secondary" onClick={downloadReminder}>Download recovery reminder</Button>
              <Button size="sm" variant="ghost" onClick={async () => {
                await api.post("/api/v1/admin/backups/reminder/dismiss");
                qc.invalidateQueries({ queryKey: ["admin-backup-settings"] });
              }}>Skip reminder</Button>
            </>
          )}
        </div>
      </article>
      <article className="mb-5 rounded-xl border border-border bg-surface-1 p-4">
        <h3 className="mb-3 font-semibold">Destinations</h3>
        <div className="mb-3 flex flex-wrap gap-6">
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={!!st.local_enabled} onCheckedChange={(v) => setForm({ ...form, local_enabled: v })} />
            This machine
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={!!st.r2_enabled} onCheckedChange={(v) => setForm({ ...form, r2_enabled: v })} />
            Cloudflare R2 / S3
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={!!st.include_media} onCheckedChange={(v) => setForm({ ...form, include_media: v })} />
            Include managed media
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={!!st.scheduled_enabled}
              onCheckedChange={(v) => {
                if (v && !passphraseSet) {
                  toast.error("Set a recovery passphrase before enabling scheduled backups.");
                  return;
                }
                setForm({ ...form, scheduled_enabled: v });
              }}
            />
            Nightly schedule
          </label>
        </div>
        {!passphraseSet && <p className="mb-3 text-sm text-muted">Scheduled backups cannot be turned on until a recovery passphrase is set.</p>}
        {st.r2_enabled && (
          <div className="mb-3 grid gap-3 md:grid-cols-2">
            <Field label="Endpoint"><Input value={st.endpoint || ""} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} placeholder="xxx.r2.cloudflarestorage.com" /></Field>
            <Field label="Bucket"><Input value={st.bucket || ""} onChange={(e) => setForm({ ...form, bucket: e.target.value })} /></Field>
            <Field label="Access key"><Input value={st.access_key || ""} onChange={(e) => setForm({ ...form, access_key: e.target.value })} /></Field>
            <Field label="Secret key" hint={st.secret_set ? "Saved. Leave blank to keep it." : "Write-only."}><Input type="password" value={form.secret_key || ""} onChange={(e) => setForm({ ...form, secret_key: e.target.value })} /></Field>
            <Field label="Prefix"><Input value={st.prefix || ""} onChange={(e) => setForm({ ...form, prefix: e.target.value })} placeholder="sounddock-backups" /></Field>
            <Field label="Region"><Input value={st.region || "auto"} onChange={(e) => setForm({ ...form, region: e.target.value })} /></Field>
          </div>
        )}
        <Button size="sm" onClick={saveSettings}>Save destinations</Button>
      </article>
      {last && (
        <div className="mb-4 rounded-xl border border-border bg-surface-1 p-4">
          <div className="text-sm text-muted">Last backup</div>
          <div className="font-medium">{last.path}</div>
          <div className="mt-1 flex flex-wrap gap-2 text-sm">
            <Badge tone={last.verified ? "success" : "warning"}>{last.verified ? "Verified" : "Unverified"}</Badge>
            <Badge>{last.status}</Badge>
            <Badge>{last.kind || "sql"}</Badge>
            <Badge>{last.destination || "local"}</Badge>
          </div>
        </div>
      )}
      <h3 className="mb-2 font-semibold">Local copies</h3>
      {restoreId && !restoreId.startsWith("remote:") && (
        <article className="mb-3 rounded-xl border border-border bg-surface-1 p-4">
          <Field label="Recovery passphrase for this restore">
            <Input type="password" value={restorePass} onChange={(e) => setRestorePass(e.target.value)} autoComplete="off" />
          </Field>
          <div className="mt-3 flex gap-2">
            <Button size="sm" onClick={async () => {
              if (!window.confirm("Restore this backup into the live database? This cannot be undone.")) return;
              try {
                const out = await api.post<any>(`/api/v1/admin/backups/${restoreId}/restore`, { confirm: true, passphrase: restorePass });
                toast.success("Restore completed. Review Restore requirements, then wait for restart.");
                setRestoreId("");
                setRestorePass("");
                if (out?.requirements) {
                  qc.setQueryData(["admin-backup-requirements"], out.requirements);
                }
                qc.invalidateQueries({ queryKey: ["admin-backup-requirements"] });
              } catch (e: any) {
                toast.error(e?.message || "Restore failed");
              }
            }}>Confirm restore</Button>
            <Button size="sm" variant="ghost" onClick={() => { setRestoreId(""); setRestorePass(""); }}>Cancel</Button>
          </div>
        </article>
      )}
      <ul className="mb-6 space-y-2">
        {(q.data || []).map((b) => (
          <li key={b.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border px-4 py-3 text-sm">
            <div className="min-w-0">
              <div className="truncate font-medium">{b.path}</div>
              <div className="text-xs text-subtle">{relativeTime(b.created_at)} · {b.kind || "sql"} · {b.destination || "local"}</div>
            </div>
            <Button size="sm" variant="secondary" onClick={() => { setRestoreId(b.id); setRestorePass(""); }}>Restore</Button>
          </li>
        ))}
      </ul>
      {st.r2_enabled && (
        <>
          <h3 className="mb-2 font-semibold">R2 copies</h3>
          <p className="mb-2 text-sm text-muted">Import onto this machine, then restore. Use this after moving to a new host. First setup can also list and import from R2.</p>
          <ul className="space-y-2">
            {(remote.data || []).map((o) => (
              <li key={o.key} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border px-4 py-3 text-sm">
                <div>
                  <div className="font-medium">{o.name || o.key}</div>
                  <div className="text-xs text-subtle">{formatBytes(o.size_bytes)} · {o.mod_time ? relativeTime(o.mod_time) : ""}</div>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" variant="secondary" onClick={async () => {
                    await api.post("/api/v1/admin/backups/import-remote", { key: o.key });
                    toast.success("Downloaded from R2");
                    qc.invalidateQueries({ queryKey: ["admin-backups"] });
                  }}>Import</Button>
                  <Button size="sm" onClick={() => { setRestoreId("remote:" + o.key); setRestorePass(""); }}>Import and restore</Button>
                </div>
              </li>
            ))}
            {restoreId.startsWith("remote:") && (
              <li className="rounded-lg border border-border px-4 py-3">
                <Field label="Recovery passphrase">
                  <Input type="password" value={restorePass} onChange={(e) => setRestorePass(e.target.value)} autoComplete="off" />
                </Field>
                <div className="mt-3 flex gap-2">
                  <Button size="sm" onClick={async () => {
                    if (!window.confirm("Download this R2 backup and restore it now? This cannot be undone.")) return;
                    await api.post("/api/v1/admin/backups/import-remote", {
                      key: restoreId.slice("remote:".length),
                      restore: true,
                      confirm: true,
                      passphrase: restorePass
                    });
                    toast.success("Imported and restored");
                    setRestoreId("");
                    qc.invalidateQueries({ queryKey: ["admin-backups"] });
                    qc.invalidateQueries({ queryKey: ["admin-backup-requirements"] });
                  }}>Confirm import and restore</Button>
                  <Button size="sm" variant="ghost" onClick={() => setRestoreId("")}>Cancel</Button>
                </div>
              </li>
            )}
            {!!settings.data?.r2_enabled && !(remote.data || []).length && !remote.isLoading && (
              <li className="text-sm text-muted">No SoundDock archives in this bucket yet.</li>
            )}
          </ul>
        </>
      )}
    </div>
  );
}

export function AdminDatabase() {
  const q = useQuery({ queryKey: ["admin-db"], queryFn: () => api.get<any>("/api/v1/admin/database") });
  const d = q.data || {};
  return (
    <div>
      <PageHeader title="Database" />
      <div className="mb-4 grid grid-cols-2 gap-3">
        <div className="rounded-xl border border-border bg-surface-1 p-4"><div className="text-sm text-muted">Size</div><div className="text-2xl font-semibold">{d.database_size || "-"}</div></div>
        <div className="rounded-xl border border-border bg-surface-1 p-4"><div className="text-sm text-muted">Migration</div><div className="text-2xl font-semibold">{d.migration_version ?? "-"}</div>{d.dirty && <Badge tone="warning">dirty</Badge>}</div>
      </div>
      <table className="w-full text-left text-sm">
        <thead className="text-muted"><tr><th className="p-2">Table</th><th className="p-2">Size</th></tr></thead>
        <tbody>{(d.tables || []).map((t: any) => <tr key={t.name} className="border-t border-border"><td className="p-2">{t.name}</td><td className="p-2">{t.size}</td></tr>)}</tbody>
      </table>
    </div>
  );
}

export function AdminDiscord() {
  const qc = useQueryClient();
  const d = useQuery({ queryKey: ["discord"], queryFn: () => api.get<any>("/api/v1/admin/integrations/discord") });
  const st = useQuery({ queryKey: ["discord-status"], queryFn: () => api.get<any>("/api/v1/admin/integrations/discord/status") });
  const guilds = useQuery({ queryKey: ["discord-guilds"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/discord/guilds") });
  const sessions = useQuery({ queryKey: ["discord-sessions"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/discord/sessions") });
  const [token, setToken] = useState("");
  const [app, setApp] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [loginOn, setLoginOn] = useState(false);
  const [guildOn, setGuildOn] = useState(false);
  const [guildId, setGuildId] = useState("");
  const [roleOn, setRoleOn] = useState(false);
  const [roleId, setRoleId] = useState("");
  const [adminIds, setAdminIds] = useState("");
  useEffect(() => {
    if (!d.data) return;
    setGuildOn(!!d.data.registration_guild_enabled);
    setGuildId(d.data.registration_guild_id || "");
    setRoleOn(!!d.data.registration_role_enabled);
    setRoleId(d.data.registration_role_id || "");
    setLoginOn(!!d.data.login_enabled);
    setClientId(d.data.client_id || "");
    setAdminIds((d.data.admin_discord_ids || []).join(", "));
  }, [d.data]);
  return (
    <div>
      <PageHeader
        title="Discord"
        description="Optional. Sign-in, bot, and who may register. Nothing here lives in .env."
        actions={
          <div className="flex flex-wrap gap-2">
            <HelpButton variant="secondary" />
            <DiscordServerButton />
          </div>
        }
      />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.data?.login_ready ? "success" : "neutral"}>{d.data?.login_ready ? "Sign-in ready" : "Sign-in off"}</Badge>
        <Badge tone={d.data?.token_configured ? "success" : "neutral"}>{d.data?.token_configured ? "Token configured" : "No token"}</Badge>
        <Badge>{d.data?.gateway_status || st.data?.gateway || "idle"}</Badge>
        <Badge>{st.data?.commands || "commands unknown"}</Badge>
      </div>
      <form className="mb-6 max-w-lg space-y-4 rounded-xl border border-border bg-surface-1 p-4" onSubmit={async (e) => {
        e.preventDefault();
        await api.put("/api/v1/admin/integrations/discord", {
          login_enabled: loginOn,
          client_id: clientId || undefined,
          client_secret: clientSecret || undefined,
          application_id: app || clientId || undefined
        });
        toast.success("Discord sign-in saved");
        setClientSecret("");
        qc.invalidateQueries({ queryKey: ["discord"] });
      }}>
        <h3 className="font-semibold">Sign-in</h3>
        <p className="text-sm text-muted">Paste this redirect URL in the Discord application OAuth2 settings. It must end with <code>/callback</code>.</p>
        <code className="block break-all rounded-lg bg-surface-2 px-3 py-2 text-xs">{d.data?.oauth_redirect || "…"}</code>
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-medium">Allow Discord sign-in</div>
            <p className="text-xs text-subtle">Off by default. Local username and password always work.</p>
          </div>
          <Switch checked={loginOn} onCheckedChange={setLoginOn} />
        </div>
        <Field label="Client ID"><Input value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="OAuth2 client ID" /></Field>
        <Field label="Client secret" hint={d.data?.secret_configured ? "Leave blank to keep the saved secret." : "Required to turn sign-in on."}>
          <Input type="password" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} placeholder="••••••••" />
        </Field>
        <Button type="submit">Save sign-in</Button>
      </form>
      <form className="mb-6 max-w-lg space-y-4 rounded-xl border border-border bg-surface-1 p-4" onSubmit={async (e) => {
        e.preventDefault();
        await api.put("/api/v1/admin/integrations/discord", {
          enabled: d.data?.enabled ?? false,
          admin_discord_ids: adminIds.split(",").map((s) => s.trim()).filter(Boolean)
        });
        toast.success("Discord administrators saved");
        qc.invalidateQueries({ queryKey: ["discord"] });
      }}>
        <h3 className="font-semibold">Administrators</h3>
        <p className="text-sm text-muted">The first Discord sign-in links to the first local administrator and stores that Discord user ID. Later Discord users keep their Discord username, display name, and ID.</p>
        <Field label="Discord user IDs" hint="Numeric snowflakes. These accounts skip the registration whitelist and get Administrator on sign-in.">
          <Input value={adminIds} onChange={(e) => setAdminIds(e.target.value)} placeholder="your Discord user ID" />
        </Field>
        <Button type="submit">Save administrators</Button>
      </form>
      <form className="mb-6 max-w-lg space-y-3 rounded-xl border border-border bg-surface-1 p-4" onSubmit={async (e) => {
        e.preventDefault();
        await api.put("/api/v1/admin/integrations/discord", {
          enabled: true,
          token: token || undefined,
          application_id: app || undefined,
          client_id: clientId || undefined
        });
        toast.success("Discord bot settings saved");
        setToken("");
        qc.invalidateQueries({ queryKey: ["discord"] });
      }}>
        <h3 className="font-semibold">Bot</h3>
        <Field label="Application ID"><Input value={app || d.data?.application_id || ""} onChange={(e) => setApp(e.target.value)} /></Field>
        <Field label="Bot token" hint="Leave blank to keep the existing token."><Input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder="••••••••" /></Field>
        <div className="flex flex-wrap gap-2">
          <Button type="submit">Save</Button>
          <Button type="button" variant="secondary" onClick={() => api.post("/api/v1/admin/integrations/discord/test").then((r: any) => toast(r.ok ? "Connection looks good" : "Not ready"))}>Test</Button>
          <Button type="button" variant="secondary" onClick={() => api.get<any>("/api/v1/admin/integrations/discord/invite").then((r) => window.open(r.url, "_blank"))}>Invite bot</Button>
          <Button type="button" variant="ghost" onClick={() => api.post("/api/v1/admin/integrations/discord/commands/sync").then(() => toast.success("Command sync requested"))}>Sync commands</Button>
        </div>
      </form>
      <form className="mb-6 max-w-lg space-y-4 rounded-xl border border-border bg-surface-1 p-4" onSubmit={async (e) => {
        e.preventDefault();
        await api.put("/api/v1/admin/integrations/discord", {
          enabled: d.data?.enabled ?? true,
          registration_guild_enabled: guildOn,
          registration_guild_id: guildId,
          registration_role_enabled: roleOn,
          registration_role_id: roleId
        });
        toast.success("Registration whitelist saved");
        qc.invalidateQueries({ queryKey: ["discord"] });
      }}>
        <h3 className="font-semibold">Registration whitelist</h3>
        <p className="text-sm text-muted">Applies to new Discord accounts only. Existing users and Discord IDs listed under Administrators can always sign in. Role checks need the bot invited.</p>
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-medium">Require Discord server</div>
            <p className="text-xs text-subtle">New users must be in this server.</p>
          </div>
          <Switch checked={guildOn} onCheckedChange={setGuildOn} />
        </div>
        <Field label="Server ID">
          <Input value={guildId} onChange={(e) => setGuildId(e.target.value)} placeholder="Discord server snowflake" disabled={!guildOn && !roleOn} />
        </Field>
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-medium">Require Discord role</div>
            <p className="text-xs text-subtle">New users must also have this role in that server.</p>
          </div>
          <Switch checked={roleOn} onCheckedChange={setRoleOn} />
        </div>
        <Field label="Role ID">
          <Input value={roleId} onChange={(e) => setRoleId(e.target.value)} placeholder="Discord role snowflake" disabled={!roleOn} />
        </Field>
        <Button type="submit">Save whitelist</Button>
      </form>
      <h3 className="mb-2 font-semibold">Servers</h3>
      <p className="mb-3 text-sm text-muted">One bot token covers every invited server. Disable a server to ignore slash commands and web play there.</p>
      {!guilds.data?.length && <p className="mb-4 text-sm text-muted">Invite the SoundDock bot to get started.</p>}
      <ul className="space-y-2">
        {(guilds.data || []).map((g) => (
          <li key={g.id} className="flex items-center justify-between gap-3 rounded-lg border border-border px-4 py-3">
            <div>
              <div className="font-medium">{g.name || g.id}</div>
              <div className="text-xs text-muted">volume {g.default_volume} · queue {g.queue_limit}</div>
            </div>
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-2">
                <span className="text-xs text-muted">{g.enabled === false ? "Off" : "On"}</span>
                <Switch
                  checked={g.enabled !== false}
                  onCheckedChange={async (on) => {
                    try {
                      await api.patch(`/api/v1/admin/integrations/discord/guilds/${g.id}`, { enabled: on });
                      toast.success(on ? "Server enabled" : "Server disabled");
                      qc.invalidateQueries({ queryKey: ["discord-guilds"] });
                    } catch (err) {
                      toast.error(err instanceof Error ? err.message : "Could not update server");
                    }
                  }}
                />
              </div>
              <Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/integrations/discord/guilds/${g.id}/disconnect`).then(() => toast("Disconnected"))}>Disconnect voice</Button>
            </div>
          </li>
        ))}
      </ul>
      {d.data?.last_error && <p className="mb-4 text-sm text-destructive">{d.data.last_error}</p>}
      <h3 className="mb-2 mt-6 font-semibold">Voice sessions</h3>
      {(sessions.data || []).map((s) => (
        <div key={s.guild_id} className="mb-2 text-sm text-muted">
          {s.guild_name || s.guild_id} · {s.connected ? "connected" : s.last_disconnect_reason || s.reason || "idle"}
          {s.session_id ? ` · ${s.session_id}` : ""}
          {s.status ? ` · ${s.status}` : ""}
          {s.current_track_id ? ` · track ${s.current_track_id}` : ""}
        </div>
      ))}
    </div>
  );
}

const apiKeyScopes = [
  { name: "admin", label: "Administrator", desc: "Logs, diagnostics, Discord, updates, users" },
  { name: "tracks.read", label: "Read catalogue", desc: "List tracks, albums, artists, search" },
  { name: "tracks.stream", label: "Stream audio", desc: "Play files from the library" },
  { name: "playlists.write", label: "Playlists", desc: "Create and edit playlists" },
  { name: "history.read", label: "History", desc: "Listen history and stats" },
  { name: "library.upload", label: "Upload", desc: "Upload into writable libraries" },
  { name: "library.import_url", label: "URL import", desc: "Import remote files" },
  { name: "library.create", label: "Create libraries", desc: "Add library roots" },
  { name: "library.migrate", label: "Migrate libraries", desc: "Move files into managed storage" }
];

export function AdminIntegrations() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-int"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations") });
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<string[]>(["admin"]);
  const [secret, setSecret] = useState("");
  const toggle = (scope: string) => {
    setScopes((cur) => (cur.includes(scope) ? cur.filter((s) => s !== scope) : [...cur, scope]));
  };
  return (
    <div>
      <PageHeader title="API keys" description="Create keys here, not on Profile. Pick scopes for each key. The secret is shown once." />
      <form
        className="mb-6 space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          if (!scopes.length) {
            toast.error("Pick at least one scope");
            return;
          }
          try {
            const r = await api.post<any>("/api/v1/admin/integrations", { name, scopes });
            setSecret(r.secret);
            setName("");
            toast.success("API key created");
            qc.invalidateQueries({ queryKey: ["admin-int"] });
          } catch (err) {
            toast.error(err instanceof Error ? err.message : "Could not create key");
          }
        }}
      >
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="cursor-debug" required />
        </Field>
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" variant="secondary" onClick={() => setScopes(["tracks.read", "tracks.stream", "history.read"])}>Read & stream</Button>
          <Button type="button" size="sm" variant="secondary" onClick={() => setScopes(["tracks.read", "tracks.stream", "playlists.write", "history.read", "library.upload"])}>Library</Button>
          <Button type="button" size="sm" variant="secondary" onClick={() => setScopes(["admin"])}>Administrator</Button>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          {apiKeyScopes.map((s) => (
            <label key={s.name} className="flex items-start gap-3 rounded-lg border border-border px-3 py-2">
              <input type="checkbox" className="mt-1" checked={scopes.includes(s.name)} onChange={() => toggle(s.name)} />
              <span>
                <span className="block text-sm font-medium">{s.label}</span>
                <span className="block text-xs text-muted">{s.desc}</span>
              </span>
            </label>
          ))}
        </div>
        <Button type="submit">Create key</Button>
      </form>
      {secret && (
        <p className="mb-4 rounded-lg bg-surface-2 p-3 text-sm">
          Copy this key now: <code className="break-all">{secret}</code>
        </p>
      )}
      <ul className="space-y-2">
        {(q.data || []).map((c) => (
          <li key={c.id} className="flex items-center justify-between rounded-lg border border-border px-4 py-3">
            <div>
              <div className="font-medium">{c.name}</div>
              <div className="text-xs text-muted">
                {c.prefix ? `${c.prefix}… · ` : ""}{(c.scopes || []).join(", ") || "no scopes"}
                {c.last_used_at ? ` · used ${relativeTime(c.last_used_at)}` : " · never used"}
              </div>
            </div>
            <Button size="sm" variant="ghost" onClick={() => api.del(`/api/v1/admin/integrations/${c.id}`).then(() => { toast("Revoked"); qc.invalidateQueries({ queryKey: ["admin-int"] }); })}>Revoke</Button>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function AdminExternalProviders() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["ext-providers"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/external-providers") });
  return (
    <div>
      <PageHeader title="External playlist providers" description="OAuth credentials are write-only. Playlist import matches your library, then downloads missing tracks from YouTube." />
      <div className="space-y-4">
        {(q.data || []).map((p) => (
          <ProviderCard key={p.provider} p={p} onSaved={() => qc.invalidateQueries({ queryKey: ["ext-providers"] })} />
        ))}
      </div>
    </div>
  );
}

function ProviderCard({ p, onSaved }: { p: any; onSaved: () => void }) {
  const [enabled, setEnabled] = useState(!!p.enabled);
  const [may, setMay] = useState(!!p.users_may_connect);
  const [pub, setPub] = useState(!!p.public_import);
  const [cid, setCid] = useState(p.client_id || "");
  const [secret, setSecret] = useState("");
  const [extra, setExtra] = useState("");
  const names: Record<string, string> = { spotify: "Spotify", youtube: "YouTube", soundcloud: "SoundCloud", apple_music: "Apple Music" };
  return (
    <form
      className="space-y-3 rounded-xl border border-border bg-surface-1 p-4"
      onSubmit={async (e) => {
        e.preventDefault();
        const body: any = { enabled, users_may_connect: may, public_import: pub, client_id: cid };
        if (secret) body.client_secret = secret;
        if (extra) {
          body.extra = p.provider === "apple_music" ? { developer_token: extra } : { api_key: extra };
        }
        await api.put(`/api/v1/admin/integrations/external-providers/${p.provider}`, body);
        toast.success("Saved");
        setSecret("");
        setExtra("");
        onSaved();
      }}
    >
      <div className="flex items-center justify-between">
        <h2 className="font-semibold">{names[p.provider] || p.provider}</h2>
        {p.has_client_secret && <Badge tone="success">Secret on file</Badge>}
      </div>
      <p className="text-xs text-subtle">Callback: <code>{p.callback_url}</code></p>
      {p.capabilities ? (
        <p className="text-xs text-subtle">
          {[
            p.capabilities.list_user_playlists && "user playlists",
            p.capabilities.private_playlists && "private playlists",
            p.capabilities.isrc && "ISRC",
            p.capabilities.snapshot && "snapshot sync"
          ].filter(Boolean).join(" · ")}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-4">
        <label className="flex items-center gap-2 text-sm"><Switch checked={enabled} onCheckedChange={setEnabled} /> Enabled</label>
        <label className="flex items-center gap-2 text-sm"><Switch checked={may} onCheckedChange={setMay} /> Users may connect</label>
        <label className="flex items-center gap-2 text-sm"><Switch checked={pub} onCheckedChange={setPub} /> Public playlist import</label>
      </div>
      {p.provider !== "apple_music" && (
        <>
          <Field label="Client ID"><Input value={cid} onChange={(e) => setCid(e.target.value)} /></Field>
          <Field label="Client secret" hint="Leave blank to keep the stored secret."><Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} /></Field>
        </>
      )}
      {p.provider === "youtube" && (
        <Field label="API key (optional, public playlists)"><Input type="password" value={extra} onChange={(e) => setExtra(e.target.value)} /></Field>
      )}
      {p.provider === "apple_music" && (
        <Field label="MusicKit developer token" hint="Paste a developer JWT. Stored encrypted.">
          <Input type="password" value={extra} onChange={(e) => setExtra(e.target.value)} />
        </Field>
      )}
      <Button type="submit" size="sm">Save</Button>
    </form>
  );
}

export function AdminWebhooks() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["webhooks"], queryFn: () => api.get<any[]>("/api/v1/admin/webhooks") });
  const [url, setUrl] = useState("");
  const [secret, setSecret] = useState("");
  return (
    <div>
      <PageHeader title="Webhooks" />
      <form className="mb-4 max-w-lg space-y-2" onSubmit={async (e) => {
        e.preventDefault();
        await api.post("/api/v1/admin/webhooks", { url, secret, events: ["playback.started", "library.scan.completed"] });
        toast.success("Webhook added");
        qc.invalidateQueries({ queryKey: ["webhooks"] });
      }}>
        <Field label="URL"><Input value={url} onChange={(e) => setUrl(e.target.value)} required /></Field>
        <Field label="Secret"><Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} /></Field>
        <Button type="submit">Add webhook</Button>
      </form>
      {(q.data || []).map((w) => (
        <div key={w.id} className="mb-2 flex items-center justify-between rounded-lg border border-border px-4 py-3">
          <div className="truncate text-sm">{w.url}</div>
          <Button size="sm" variant="ghost" onClick={() => api.del(`/api/v1/admin/webhooks/${w.id}`).then(() => qc.invalidateQueries({ queryKey: ["webhooks"] }))}>Delete</Button>
        </div>
      ))}
    </div>
  );
}

function metadataJobTone(status?: string) {
  if (status === "failed") return "danger" as const;
  if (status === "completed") return "success" as const;
  if (status === "cancelled") return "warning" as const;
  if (status === "running") return "accent" as const;
  return "neutral" as const;
}

export function AdminMetadata() {
  const qc = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const q = useQuery({
    queryKey: ["admin-meta"],
    queryFn: () => api.get<{
      external_enabled?: boolean;
      providers?: string[];
      track_count?: number;
      busy?: boolean;
      job?: {
        id: string;
        status: string;
        progress: number;
        last_error?: string | null;
        created_at: string;
        started_at?: string | null;
        finished_at?: string | null;
        result?: { total?: number; done?: number; updated?: number; skipped?: number; failed?: number; covers?: number; unmatched?: number };
      } | null;
    }>("/api/v1/admin/metadata"),
    refetchInterval: (query) => (query.state.data?.busy ? 1000 : 8000),
  });

  const d = q.data;
  const job = d?.job;
  const busy = !!d?.busy;
  const tracks = d?.track_count ?? 0;
  const result = job?.result;
  const done = result?.done ?? 0;
  const total = result?.total ?? tracks;
  const pct = typeof job?.progress === "number" ? job.progress : 0;

  async function refreshAll() {
    setSubmitting(true);
    try {
      await api.post("/api/v1/admin/metadata/refresh");
      toast.success("Library metadata update started");
      await qc.invalidateQueries({ queryKey: ["admin-meta"] });
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 409) {
        toast.error("An update is already running");
        await q.refetch();
      } else {
        toast.error(err.message || "Could not start metadata update");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div>
      <PageHeader title="Metadata" description="MusicBrainz for titles, artists, genres, and MBIDs. Cover Art Archive for release artwork." />
      <div className="mb-4 flex items-center gap-3 rounded-xl border border-border bg-surface-1 p-4">
        <Switch checked={!!d?.external_enabled} onCheckedChange={(v) => api.put("/api/v1/admin/metadata", { external_enabled: v }).then(() => { toast.success("Settings saved"); q.refetch(); })} />
        <div>
          <div className="font-medium">External providers</div>
          <div className="text-sm text-muted">{(d?.providers || []).join(", ") || "musicbrainz, coverartarchive"}</div>
        </div>
      </div>

      <section className="rounded-xl border border-border bg-surface-1 p-4">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="font-semibold">Update all library metadata</h2>
            <p className="mt-1 text-sm text-muted">
              Looks up every track on MusicBrainz and fills missing tags, genres, artist IDs, and cover art. Locked tracks are skipped.
              {tracks > 0 && (
                <> About {tracks} track{tracks === 1 ? "" : "s"} - MusicBrainz allows one request per second, so a full pass can take several minutes.</>
              )}
            </p>
          </div>
          <Button onClick={refreshAll} disabled={busy || submitting}>
            {busy ? "Updating library…" : submitting ? "Starting…" : "Update all music"}
          </Button>
        </div>
        {job ? (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <Badge tone={metadataJobTone(job.status)}>{job.status}</Badge>
              {busy && <Badge tone="accent">In progress</Badge>}
              <span className="text-muted">
                {busy || done > 0 ? `${done} / ${total || tracks} tracks` : `${tracks} tracks in library`}
              </span>
              <span className="text-muted">{pct}%</span>
            </div>
            <div className="h-3 w-full overflow-hidden rounded-full bg-surface-3">
              <div className="h-full bg-accent transition-all" style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} />
            </div>
            <dl className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
              {result?.updated != null && (
                <div><dt className="text-muted">Updated</dt><dd className="font-medium">{result.updated}</dd></div>
              )}
              {result?.covers != null && (
                <div><dt className="text-muted">Covers added</dt><dd className="font-medium">{result.covers}</dd></div>
              )}
              {result?.skipped != null && (
                <div><dt className="text-muted">Skipped</dt><dd className="font-medium">{result.skipped}</dd></div>
              )}
              {result?.failed != null && (
                <div><dt className="text-muted">Failed</dt><dd className="font-medium">{result.failed}</dd></div>
              )}
            </dl>
            <p className="text-xs text-subtle">Queued {relativeTime(job.created_at)}</p>
            {job.last_error && <p className="text-sm text-destructive">{job.last_error}</p>}
          </div>
        ) : (
          <p className="text-sm text-muted">No library-wide update has been run yet.</p>
        )}
      </section>
    </div>
  );
}

export function AdminTranscode() {
  const q = useQuery({ queryKey: ["tx"], queryFn: () => api.get<any>("/api/v1/admin/transcode") });
  return (
    <div>
      <PageHeader title="Transcoding" actions={<Button variant="secondary" onClick={() => api.del("/api/v1/admin/transcode/cache").then(() => toast.success("Cache cleared"))}>Clear cache</Button>} />
      <div className="grid grid-cols-2 gap-3">
        {Object.entries(q.data || {}).filter(([, v]) => typeof v !== "object").map(([k, v]) => (
          <div key={k} className="rounded-xl border border-border bg-surface-1 p-4">
            <div className="text-sm capitalize text-muted">{k.replaceAll("_", " ")}</div>
            <div className="text-xl font-semibold">{String(v)}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

export { AdminRetention } from "./AdminRetention";

export function AdminSecurity() {
  const q = useQuery({ queryKey: ["audit"], queryFn: () => api.get<any[]>("/api/v1/admin/audit") });
  return (
    <div>
      <PageHeader title="Security" description="Recent audit events." />
      <ul className="space-y-1 text-sm">
        {(q.data || []).map((e) => (
          <li key={e.id} className="rounded-md border border-border px-3 py-2">
            <span className="font-medium">{e.action}</span> <span className="text-muted">{e.target}</span>
            <span className="float-right text-xs text-subtle">{relativeTime(e.created_at)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function AdminLogs() {
  const [level, setLevel] = useState("");
  const [category, setCategory] = useState("");
  const [search, setSearch] = useState("");
  const [qtext, setQtext] = useState("");
  const [cursor, setCursor] = useState("");
  const [pages, setPages] = useState<any[]>([]);
  const q = useQuery({
    queryKey: ["logs", level, category, qtext, cursor],
    queryFn: () => api.get<{ items?: any[]; next_cursor?: string }>(`/api/v1/admin/logs?level=${encodeURIComponent(level)}&category=${encodeURIComponent(category)}&q=${encodeURIComponent(qtext)}&cursor=${encodeURIComponent(cursor)}&limit=50`)
  });
  const items = cursor ? pages : (q.data?.items || []);
  const [open, setOpen] = useState<string | null>(null);
  return (
    <div>
      <PageHeader title="Logs" description="Filter and search operational logs. Job entries keep a plain-language explanation." />
      <form className="mb-4 flex flex-wrap gap-2" onSubmit={(e) => { e.preventDefault(); setCursor(""); setPages([]); setQtext(search); }}>
        <input className="rounded-md border border-border bg-surface-1 px-3 py-2 text-sm" placeholder="Search" value={search} onChange={(e) => setSearch(e.target.value)} />
        <select className="rounded-md border border-border bg-surface-1 px-3 py-2 text-sm" value={level} onChange={(e) => { setLevel(e.target.value); setCursor(""); setPages([]); }}>
          <option value="">All levels</option>
          <option value="debug">debug</option>
          <option value="info">info</option>
          <option value="warn">warn</option>
          <option value="error">error</option>
        </select>
        <input className="rounded-md border border-border bg-surface-1 px-3 py-2 text-sm" placeholder="Category" value={category} onChange={(e) => { setCategory(e.target.value); setCursor(""); setPages([]); }} />
        <Button type="submit" variant="secondary">Search</Button>
      </form>
      <ul className="space-y-2">
        {items.map((l: any, i: number) => (
          <li key={l.id || i} className="rounded-lg border border-border px-4 py-3 text-sm">
            <div className="font-medium">{l.summary || l.category || l.type}</div>
            <div className="text-destructive">{l.error || l.message}</div>
            <div className="text-xs text-subtle">{l.level} · {relativeTime(l.at || l.created_at)}</div>
            {l.detail && l.detail !== l.error && (
              <button className="mt-2 text-xs text-muted underline" onClick={() => setOpen(open === (l.id || String(i)) ? null : (l.id || String(i)))}>
                {open === (l.id || String(i)) ? "Hide technical detail" : "Show technical detail"}
              </button>
            )}
            {open === (l.id || String(i)) && l.detail && (
              <pre className="mt-2 max-h-32 overflow-auto rounded-md bg-surface-2 p-2 font-mono text-[11px] text-muted">{l.detail}</pre>
            )}
          </li>
        ))}
        {!items.length && !q.isLoading && <li className="text-sm text-muted">No matching log entries.</li>}
      </ul>
      {q.data?.next_cursor && (
        <Button className="mt-3" variant="secondary" onClick={() => {
          setPages(items.concat(q.data?.items || []));
          setCursor(q.data?.next_cursor || "");
        }}>Load more</Button>
      )}
    </div>
  );
}

export function AdminUpdates() {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [watch, setWatch] = useState(false);
  const q = useQuery({
    queryKey: ["admin-updates"],
    queryFn: () => api.get<any>("/api/v1/admin/updates"),
    refetchInterval: (query) => {
      const cur = query.state.data as { updating?: boolean; last_status?: string } | undefined;
      if (watch || cur?.updating || cur?.last_status === "updating") return 1000;
      return 8000;
    }
  });
  const d = q.data || {};
  const updating = !!(d.updating || d.last_status === "updating" || d.progress?.stage === "pulling" || d.progress?.stage === "restarting" || d.progress?.stage === "queued");
  const changelog = Array.isArray(d.changelog) ? d.changelog : [];
  const pct = typeof d.progress?.percent === "number" ? d.progress.percent : updating ? 12 : 0;

  useEffect(() => {
    if (!watch) return;
    if (updating) return;
    if (d.last_status === "error" || d.last_status === "needs_recovery") {
      setWatch(false);
      toast.error(d.last_error || (d.last_status === "needs_recovery" ? "Update needs recovery" : "Update failed"));
      return;
    }
    if (d.last_status === "ok") {
      setWatch(false);
      toast.success(`Updated to ${d.version || "the latest image"}`);
      const t = setTimeout(() => location.reload(), 800);
      return () => clearTimeout(t);
    }
  }, [watch, updating, d.last_status, d.last_error, d.version]);

  return (
    <div>
      <PageHeader title="Updates" description="Check now talks to GitHub and works on any host. Update now prefers the installer helper. The Docker socket is used only when SD_ALLOW_DOCKER_SOCK is on. Postgres is left running." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.available ? "warning" : "success"}>{d.available ? "Update available" : "Up to date"}</Badge>
        {updating ? <Badge tone="accent">Updating</Badge> : d.can_apply ? <Badge tone="success">Can install</Badge> : <Badge tone="warning">Cannot install here</Badge>}
        {d.helper_ok ? <Badge tone="success">Host helper</Badge> : <Badge>No host helper</Badge>}
        {d.socket_ok ? <Badge tone="success">Docker socket</Badge> : <Badge>No Docker socket</Badge>}
        {d.schema_forward_only ? <Badge tone="warning">Schema forward only</Badge> : d.reversible ? <Badge tone="success">Reversible</Badge> : null}
        {d.needs_recovery ? <Badge tone="danger">Needs recovery</Badge> : <Badge>{d.last_status || "idle"}</Badge>}
      </div>
      <div className="mb-4 rounded-xl border border-border bg-surface-1 p-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <div className="text-sm text-muted">Installed</div>
            <div className="text-2xl font-semibold">{d.version || "0.0.1"}</div>
          </div>
          <div>
            <div className="text-sm text-muted">Latest</div>
            <div className="text-2xl font-semibold">{d.latest_version || d.version || "0.0.1"}</div>
          </div>
        </div>
        <p className="mt-1 break-all text-xs text-subtle">{d.image}</p>
        <p className="mt-3 text-sm text-muted">Last check: {d.last_check_at ? relativeTime(d.last_check_at) : "never"}</p>
        <p className="text-sm text-muted">Last update: {d.last_applied_at ? relativeTime(d.last_applied_at) : "never"}{d.last_applied_by ? ` (${d.last_applied_by})` : ""}</p>
        {d.last_error && <p className="mt-2 text-sm text-destructive">{d.last_error}</p>}
        {d.needs_recovery && (
          <p className="mt-2 text-sm text-destructive">
            This update changed the database schema and then failed. The previous image was not started. Restore from the pre-update SQL backup{d.backup_path ? ` (${d.backup_path})` : ""} using Admin, Backups.
          </p>
        )}
        {d.apply_reason && <p className="mt-2 text-sm text-muted">{d.apply_reason}</p>}
        {!d.can_apply && !d.apply_reason && <p className="mt-2 text-sm text-muted">Neither the host helper nor an opted-in Docker socket is available. Re-run the installer so sounddock-update can pull images on the host.</p>}

        {d.available && !updating && (
          <div className="mt-4 rounded-lg border border-border bg-surface-2 p-4">
            <div className="text-sm font-medium">
              {d.latest_version && d.latest_version !== d.version ? `Version ${d.latest_version}` : "Newer image"}
            </div>
            {changelog.length ? (
              <ul className="mt-3 space-y-3">
                {changelog.map((rel: { version: string; notes: string[] }) => (
                  <li key={rel.version}>
                    <div className="text-xs font-semibold text-muted">{rel.version}</div>
                    <ul className="mt-1 list-disc space-y-1 pl-5 text-sm">
                      {(rel.notes || []).map((n: string, i: number) => <li key={i}>{n}</li>)}
                    </ul>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-muted">A newer image is ready. Changelog will show after Check now can reach GitHub.</p>
            )}
          </div>
        )}

        {updating && (
          <div className="mt-4 space-y-2">
            <div className="flex items-center justify-between text-sm">
              <span className="font-medium">{d.progress?.stage === "restarting" ? "Starting new containers" : d.progress?.stage === "queued" ? "Waiting for the host" : "Pulling image"}</span>
              <span className="text-muted">{pct}%</span>
            </div>
            <Progress value={pct} />
            <p className="text-xs text-subtle">{d.progress?.detail || "The host is pulling the latest image."}</p>
            {d.progress?.log && (
              <pre className="max-h-36 overflow-auto rounded-md bg-surface-2 p-3 font-mono text-[11px] leading-4 text-muted">{d.progress.log}</pre>
            )}
          </div>
        )}

        <div className="mt-4 flex flex-wrap gap-2">
          <Button variant="secondary" disabled={busy || d.checking || updating} onClick={async () => {
            setBusy(true);
            try {
              await api.post("/api/v1/admin/updates/check");
              qc.invalidateQueries({ queryKey: ["admin-updates"] });
              toast.success("Checked for updates");
            } catch {
              toast.error("Could not check for updates");
            } finally {
              setBusy(false);
            }
          }}>Check now</Button>
          <Button disabled={busy || updating || !d.can_apply || !d.available || d.needs_recovery} onClick={async () => {
            setBusy(true);
            try {
              await api.post("/api/v1/admin/updates/apply");
              setWatch(true);
              toast.success("Host is pulling the new image");
              qc.invalidateQueries({ queryKey: ["admin-updates"] });
            } catch (e: any) {
              toast.error(e?.message || "Could not start update");
            } finally {
              setBusy(false);
            }
          }}>{updating ? "Updating…" : "Update now"}</Button>
        </div>
      </div>
      <div className="flex max-w-lg items-center justify-between rounded-xl border border-border bg-surface-1 p-4">
        <div>
          <div className="text-sm font-medium">Automatic updates</div>
          <p className="text-xs text-subtle">When on, SoundDock checks about once an hour and pulls a newer image on the host.</p>
        </div>
        <Switch checked={!!d.auto_enabled} onCheckedChange={(v) => api.put("/api/v1/admin/updates", { auto_enabled: v }).then(() => { toast.success(v ? "Automatic updates on" : "Automatic updates off"); qc.invalidateQueries({ queryKey: ["admin-updates"] }); })} />
      </div>
    </div>
  );
}

export function AdminCloudflare() {
  return (
    <div>
      <PageHeader title="Cloudflare" description="Trusted proxy headers are applied when SoundDock sits behind Cloudflare Tunnel or another reverse proxy." />
      <div className="rounded-xl border border-border bg-surface-1 p-5 text-sm text-muted">
        <p>If you terminate TLS at Cloudflare, set the instance public URL and cookie security in server configuration. Client IP and proto are read from <code>CF-Connecting-IP</code> and <code>X-Forwarded-Proto</code> when present.</p>
        <p className="mt-3">There is nothing to store in the database for Cloudflare itself. Install cloudflared as a systemd service on the host. Point the tunnel at http://localhost:8080.</p>
      </div>
    </div>
  );
}

export { AdminHealth } from "./AdminHealth";
export { AdminQuotas } from "./AdminQuotas";
export { AdminMaintenance } from "./AdminMaintenance";
export { AdminDiagnostics } from "./AdminDiagnostics";
export { AdminGrants } from "./AdminGrants";

