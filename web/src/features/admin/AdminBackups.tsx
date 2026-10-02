import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CloudDownload } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { formatBytes, relativeTime } from "@/lib/utils";
import { AdminTabs, Card, RowMenu, SaveBar, SettingSwitch, errorMessage } from "./adminUi";

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

type RestoreRequirement = { key: string; class: string; source: string; present_on_host?: boolean; recovered?: boolean; note?: string };

type BackupRow = { id: string; path: string; created_at: string; kind?: string; destination?: string; status?: string; verified?: boolean };
type RemoteRow = { key: string; name?: string; size_bytes?: number; mod_time?: string };

type RestoreTarget = { kind: "local"; backup: BackupRow } | { kind: "remote"; remote: RemoteRow };

const settingsKey = ["admin-backup-settings"];

export function AdminBackups() {
  const settings = useQuery({ queryKey: settingsKey, queryFn: () => api.get<BackupSettings>("/api/v1/admin/backups/settings") });
  const needsPassphrase = settings.data && !settings.data.restore_passphrase_set;
  return (
    <div>
      <PageHeader title="Backups" description="Encrypted backups of the database, managed media, artwork, and lyrics. Network library folders are not included." />
      <AdminTabs
        fallback="backups"
        tabs={[
          { id: "backups", label: "Backups", content: <BackupsTab settings={settings.data} /> },
          { id: "settings", label: "Settings", badge: needsPassphrase ? <Badge tone="warning">Action needed</Badge> : undefined, content: <SettingsTab settings={settings.data} /> }
        ]}
      />
    </div>
  );
}

function BackupsTab({ settings }: { settings?: BackupSettings }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-backups"], queryFn: () => api.get<BackupRow[]>("/api/v1/admin/backups") });
  const reqs = useQuery({ queryKey: ["admin-backup-requirements"], queryFn: () => api.get<{ items?: RestoreRequirement[] }>("/api/v1/admin/backups/restore-requirements") });
  const remote = useQuery({ queryKey: ["admin-backup-remote"], queryFn: () => api.get<RemoteRow[]>("/api/v1/admin/backups/remote"), enabled: !!settings?.r2_enabled });
  const [busy, setBusy] = useState(false);
  const [restore, setRestore] = useState<RestoreTarget | null>(null);
  const passphraseSet = !!settings?.restore_passphrase_set;
  const last = q.data?.[0];

  async function runBackup() {
    setBusy(true);
    try {
      await api.post("/api/v1/admin/backups");
      toast.success("Backup finished");
      qc.invalidateQueries({ queryKey: ["admin-backups"] });
      qc.invalidateQueries({ queryKey: ["admin-backup-remote"] });
    } catch (e) {
      toast.error(errorMessage(e, "Backup failed"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-5">
      {!passphraseSet && settings && (
        <div className="rounded-xl border border-warning/40 bg-warning/10 p-4 text-sm">Set a recovery passphrase on the Settings tab before the first backup.</div>
      )}
      {(reqs.data?.items || []).length > 0 && (
        <Card
          title="After restoring"
          description="Values recovered from the backup, and what this server still needs. Network library mounts and the public URL are never copied from a backup."
          actions={<Button size="sm" variant="secondary" onClick={async () => { await api.post("/api/v1/admin/backups/restore-requirements/dismiss"); qc.invalidateQueries({ queryKey: ["admin-backup-requirements"] }); }}>Dismiss</Button>}
        >
          <ul className="space-y-2 text-sm">
            {reqs.data!.items!.map((it) => (
              <li key={it.key} className="rounded-lg border border-border px-3 py-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{it.key}</span>
                  <Badge tone={it.recovered ? "success" : "warning"}>{it.class}</Badge>
                  {it.present_on_host ? <Badge>on this server</Badge> : null}
                </div>
                {it.note && <p className="mt-1 text-xs text-subtle">{it.note}</p>}
              </li>
            ))}
          </ul>
        </Card>
      )}

      <Card
        title="Backups on this server"
        description={last ? `Last backup ${relativeTime(last.created_at)}${last.verified ? ", verified" : ""}.` : "No backups yet."}
        actions={<Button disabled={busy || !passphraseSet} onClick={runBackup}>{busy ? "Backing up…" : "Back up now"}</Button>}
      >
        <ul className="divide-y divide-border rounded-lg border border-border">
          {(q.data || []).map((b) => (
            <li key={b.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
              <div className="min-w-0">
                <div className="truncate font-medium">{b.path}</div>
                <div className="flex flex-wrap items-center gap-2 text-xs text-subtle">
                  {relativeTime(b.created_at)} · {b.destination || "local"}
                  {b.verified ? <Badge tone="success">Verified</Badge> : b.status && b.status !== "completed" ? <Badge tone="warning">{b.status}</Badge> : null}
                </div>
              </div>
              <Button size="sm" variant="secondary" onClick={() => setRestore({ kind: "local", backup: b })}>Restore</Button>
            </li>
          ))}
          {!q.data?.length && <li className="px-3 py-4 text-center text-sm text-muted">{q.isLoading ? "Loading…" : "Nothing here yet."}</li>}
        </ul>
      </Card>

      {settings?.r2_enabled && (
        <Card title="Backups in cloud storage" description="Download a copy to this server, or restore from it directly when moving to a new server.">
          <ul className="divide-y divide-border rounded-lg border border-border">
            {(remote.data || []).map((o) => (
              <li key={o.key} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
                <div className="min-w-0">
                  <div className="truncate font-medium">{o.name || o.key}</div>
                  <div className="text-xs text-subtle">{formatBytes(o.size_bytes)}{o.mod_time ? ` · ${relativeTime(o.mod_time)}` : ""}</div>
                </div>
                <div className="flex items-center gap-1">
                  <Button size="sm" variant="secondary" onClick={() => setRestore({ kind: "remote", remote: o })}>Restore</Button>
                  <RowMenu
                    actions={[{
                      label: "Download to this server",
                      icon: CloudDownload,
                      onSelect: async () => {
                        try {
                          await api.post("/api/v1/admin/backups/import-remote", { key: o.key });
                          toast.success("Downloaded");
                          qc.invalidateQueries({ queryKey: ["admin-backups"] });
                        } catch (e) {
                          toast.error(errorMessage(e, "Download failed"));
                        }
                      }
                    }]}
                  />
                </div>
              </li>
            ))}
            {!remote.data?.length && <li className="px-3 py-4 text-center text-sm text-muted">{remote.isLoading ? "Loading…" : "No SoundDock backups in this bucket yet."}</li>}
          </ul>
        </Card>
      )}

      <RestoreDialog target={restore} onClose={() => setRestore(null)} />
    </div>
  );
}

function RestoreDialog({ target, onClose }: { target: RestoreTarget | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [pass, setPass] = useState("");
  const [busy, setBusy] = useState(false);
  const name = target?.kind === "local" ? target.backup.path : target?.remote.name || target?.remote.key;
  return (
    <Dialog open={!!target} onOpenChange={(v) => { if (!v) { setPass(""); onClose(); } }}>
      <DialogContent title="Restore backup">
        {target && (
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              try {
                if (target.kind === "local") {
                  const out = await api.post<any>(`/api/v1/admin/backups/${target.backup.id}/restore`, { confirm: true, passphrase: pass });
                  if (out?.requirements) qc.setQueryData(["admin-backup-requirements"], out.requirements);
                } else {
                  await api.post("/api/v1/admin/backups/import-remote", { key: target.remote.key, restore: true, confirm: true, passphrase: pass });
                }
                toast.success("Restore finished. Review the notes on the Backups tab, then wait for the restart.");
                qc.invalidateQueries({ queryKey: ["admin-backups"] });
                qc.invalidateQueries({ queryKey: ["admin-backup-requirements"] });
                setPass("");
                onClose();
              } catch (err) {
                toast.error(errorMessage(err, "Restore failed"));
              } finally {
                setBusy(false);
              }
            }}
          >
            <p className="break-all text-sm text-muted">Replace everything on this server with <span className="font-medium text-foreground">{name}</span>. This cannot be undone.</p>
            <Field label="Recovery passphrase">
              <Input type="password" value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="off" required />
            </Field>
            <div className="flex gap-2">
              <Button type="submit" variant="destructive" disabled={busy || !pass}>{busy ? "Restoring…" : "Restore"}</Button>
              <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function SettingsTab({ settings }: { settings?: BackupSettings }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<BackupSettings>({});
  const [saving, setSaving] = useState(false);
  const [passphrase, setPassphrase] = useState("");
  const [currentPass, setCurrentPass] = useState("");
  const passphraseSet = !!settings?.restore_passphrase_set;
  const st = { local_enabled: true, include_media: true, use_ssl: true, ...settings, ...form };
  const dirty = Object.keys(form).length > 0;

  async function saveSettings() {
    setSaving(true);
    try {
      await api.put("/api/v1/admin/backups/settings", st);
      toast.success("Backup settings saved");
      setForm({});
      qc.invalidateQueries({ queryKey: settingsKey });
      qc.invalidateQueries({ queryKey: ["admin-backup-remote"] });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save settings"));
    } finally {
      setSaving(false);
    }
  }

  async function downloadReminder() {
    const res = await fetch("/api/v1/admin/backups/reminder", { credentials: "include" });
    if (!res.ok) {
      toast.error("Reminder is not available");
      return;
    }
    const url = URL.createObjectURL(new Blob([await res.text()], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "sounddock-recovery-reminder.txt";
    a.click();
    URL.revokeObjectURL(url);
    qc.invalidateQueries({ queryKey: settingsKey });
  }

  return (
    <div className="space-y-5">
      <Card
        title="Recovery passphrase"
        description={`SoundDock never stores this passphrase; you need it to restore. Scheduled backups run without asking for it.${passphraseSet ? " Changing it applies to new backups; older ones still need the old passphrase." : ""}`}
      >
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await api.post("/api/v1/admin/backups/passphrase", { passphrase, current_passphrase: currentPass });
              setPassphrase("");
              setCurrentPass("");
              toast.success(passphraseSet ? "Passphrase changed" : "Recovery passphrase set");
              qc.invalidateQueries({ queryKey: settingsKey });
            } catch (err) {
              toast.error(errorMessage(err, "Could not set passphrase"));
            }
          }}
        >
          <div className="grid gap-3 md:grid-cols-2">
            {passphraseSet && (
              <Field label="Current passphrase"><Input type="password" value={currentPass} onChange={(e) => setCurrentPass(e.target.value)} autoComplete="off" /></Field>
            )}
            <Field label={passphraseSet ? "New passphrase" : "Passphrase"} hint="At least 12 characters.">
              <Input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} autoComplete="new-password" />
            </Field>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" size="sm" disabled={passphrase.length < 12}>{passphraseSet ? "Change passphrase" : "Set passphrase"}</Button>
            {settings?.reminder_pending && (
              <>
                <Button type="button" size="sm" variant="secondary" onClick={downloadReminder}>Download recovery reminder</Button>
                <Button type="button" size="sm" variant="ghost" onClick={async () => { await api.post("/api/v1/admin/backups/reminder/dismiss"); qc.invalidateQueries({ queryKey: settingsKey }); }}>Skip reminder</Button>
              </>
            )}
          </div>
        </form>
      </Card>

      <Card title="Where and when">
        <div className="divide-y divide-border">
          <SettingSwitch label="Keep a copy on this server" checked={!!st.local_enabled} onChange={(v) => setForm({ ...form, local_enabled: v })} />
          <SettingSwitch label="Copy to Cloudflare R2 or S3" checked={!!st.r2_enabled} onChange={(v) => setForm({ ...form, r2_enabled: v })} />
          <SettingSwitch label="Include managed media" description="Downloaded and uploaded music. Turn off for a database-only backup." checked={!!st.include_media} onChange={(v) => setForm({ ...form, include_media: v })} />
          <SettingSwitch
            label="Back up every night"
            description={passphraseSet ? undefined : "Needs a recovery passphrase first."}
            checked={!!st.scheduled_enabled}
            disabled={!passphraseSet}
            onChange={(v) => setForm({ ...form, scheduled_enabled: v })}
          />
        </div>
        {st.r2_enabled && (
          <div className="mt-4 grid gap-3 md:grid-cols-2">
            <Field label="Endpoint"><Input value={st.endpoint || ""} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} placeholder="xxx.r2.cloudflarestorage.com" /></Field>
            <Field label="Bucket"><Input value={st.bucket || ""} onChange={(e) => setForm({ ...form, bucket: e.target.value })} /></Field>
            <Field label="Access key"><Input value={st.access_key || ""} onChange={(e) => setForm({ ...form, access_key: e.target.value })} /></Field>
            <Field label="Secret key" hint={st.secret_set ? "Saved. Leave blank to keep it." : undefined}><Input type="password" value={form.secret_key || ""} onChange={(e) => setForm({ ...form, secret_key: e.target.value })} /></Field>
            <Field label="Folder in bucket"><Input value={st.prefix || ""} onChange={(e) => setForm({ ...form, prefix: e.target.value })} placeholder="sounddock-backups" /></Field>
            <Field label="Region"><Input value={st.region || "auto"} onChange={(e) => setForm({ ...form, region: e.target.value })} /></Field>
          </div>
        )}
      </Card>
      <SaveBar dirty={dirty} saving={saving} onSave={saveSettings} onReset={() => setForm({})} />
    </div>
  );
}
