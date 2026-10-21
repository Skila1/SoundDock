import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CloudDownload, DatabaseBackup, KeyRound, Loader2, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge, Skeleton } from "@/components/ui/misc";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { formatBytes, relativeTime } from "@/lib/utils";
import { Card, RowMenu, SaveBar, SettingSwitch, errorMessage } from "./adminUi";
import { Expander, StatusLine, whenLabel } from "./storage/storageUi";

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

type BackupRow = { id: string; path: string; size_bytes?: number; created_at: string; kind?: string; destination?: string; remote_key?: string; status?: string; verified?: boolean | null };
type RemoteRow = { key: string; name?: string; size_bytes?: number; mod_time?: string };

type Location = "server" | "cloud" | "both";
type Entry = { id: string; name: string; when?: string; size?: number; location: Location; verified?: boolean | null; status?: string; local?: BackupRow; remoteKey?: string };
type RestoreTarget = { name: string; local?: BackupRow; remoteKey?: string };

const settingsKey = ["admin-backup-settings"];
const locationLabel: Record<Location, string> = { server: "This server", cloud: "Cloud", both: "Server + cloud" };

/** Scheduled backups start on the 15 minute tick once 20 hours have passed since the last one. */
function nextRun(last?: string) {
  if (!last) return new Date(Date.now() + 15 * 60 * 1000).toISOString();
  const t = new Date(last).getTime() + 20 * 3600 * 1000;
  return new Date(Math.max(t, Date.now() + 60 * 1000)).toISOString();
}

function baseName(p: string) {
  return p.split(/[\\/]/).pop() || p;
}

export function AdminBackups() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: settingsKey, queryFn: () => api.get<BackupSettings>("/api/v1/admin/backups/settings") });
  const list = useQuery({ queryKey: ["admin-backups"], queryFn: () => api.get<BackupRow[]>("/api/v1/admin/backups") });
  const cloudOn = !!settings.data?.r2_enabled;
  const remote = useQuery({ queryKey: ["admin-backup-remote"], queryFn: () => api.get<RemoteRow[]>("/api/v1/admin/backups/remote"), enabled: cloudOn });
  const reqs = useQuery({ queryKey: ["admin-backup-requirements"], queryFn: () => api.get<{ items?: RestoreRequirement[] }>("/api/v1/admin/backups/restore-requirements") });
  const [busy, setBusy] = useState(false);
  const [restore, setRestore] = useState<RestoreTarget | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const s = settings.data;
  const passphraseSet = !!s?.restore_passphrase_set;

  const entries = useMemo<Entry[]>(() => {
    const out: Entry[] = [];
    const seen = new Set<string>();
    for (const b of list.data || []) {
      const dest = b.destination || "local";
      const location: Location = dest === "both" ? "both" : dest === "r2" ? "cloud" : "server";
      if (b.remote_key) seen.add(b.remote_key);
      out.push({
        id: b.id,
        name: baseName(b.path),
        when: b.created_at,
        size: b.size_bytes,
        location,
        verified: b.verified,
        status: b.status,
        local: location === "cloud" ? undefined : b,
        remoteKey: b.remote_key || undefined
      });
    }
    for (const o of remote.data || []) {
      if (seen.has(o.key)) continue;
      out.push({ id: `r:${o.key}`, name: o.name || baseName(o.key), when: o.mod_time, size: o.size_bytes, location: "cloud", remoteKey: o.key });
    }
    return out.sort((a, b) => (b.when || "").localeCompare(a.when || ""));
  }, [list.data, remote.data]);

  const last = list.data?.[0];
  const where = !s ? "" : s.local_enabled !== false && cloudOn ? `this server and ${s.bucket || "cloud storage"}` : cloudOn ? s.bucket || "cloud storage" : "this server";

  async function runBackup() {
    setBusy(true);
    try {
      await api.post("/api/v1/admin/backups");
      toast.success("Backup finished");
      void qc.invalidateQueries({ queryKey: ["admin-backups"] });
      void qc.invalidateQueries({ queryKey: ["admin-backup-remote"] });
    } catch (e) {
      toast.error(errorMessage(e, "Backup failed"));
    } finally {
      setBusy(false);
    }
  }

  async function downloadToServer(key: string) {
    try {
      await api.post("/api/v1/admin/backups/import-remote", { key });
      toast.success("Copied to this server");
      void qc.invalidateQueries({ queryKey: ["admin-backups"] });
    } catch (e) {
      toast.error(errorMessage(e, "Download failed"));
    }
  }

  const openSettings = () => {
    setSettingsOpen(true);
    window.setTimeout(() => document.getElementById("backup-settings")?.scrollIntoView({ behavior: "smooth" }), 0);
  };

  return (
    <div className="space-y-5">
      <PageHeader
        title="Backups"
        description="Encrypted copies of the database, downloaded music, artwork and lyrics. Network library folders are not included."
        actions={
          <Button disabled={busy || !passphraseSet} onClick={() => void runBackup()}>
            {busy ? <Loader2 className="animate-spin" /> : <DatabaseBackup />} {busy ? "Backing up…" : "Back up now"}
          </Button>
        }
      />

      {s && !passphraseSet && (
        <div className="flex flex-col gap-3 rounded-2xl border border-warning/40 bg-warning/10 p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-start gap-3 text-sm">
            <KeyRound className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
            <span>Set a recovery passphrase to start backing up. You need it to restore, and SoundDock never stores it.</span>
          </div>
          <Button size="sm" variant="secondary" onClick={openSettings}>Set passphrase</Button>
        </div>
      )}

      {(reqs.data?.items || []).length > 0 && (
        <Card
          title="After restoring"
          description="Values recovered from the backup, and what this server still needs. Network library mounts and the public URL are never copied from a backup."
          actions={<Button size="sm" variant="secondary" onClick={async () => { await api.post("/api/v1/admin/backups/restore-requirements/dismiss"); void qc.invalidateQueries({ queryKey: ["admin-backup-requirements"] }); }}>Dismiss</Button>}
        >
          <ul className="space-y-2 text-sm">
            {(reqs.data?.items || []).map((it) => (
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

      <Card>
        {s ? (
          <StatusLine
            items={[
              <span key="l">
                Last backup <span className="font-medium text-foreground">{last ? relativeTime(last.created_at) : "never"}</span>
                {last?.verified ? ", verified" : last?.verified === false ? ", check failed" : ""}
              </span>,
              <span key="n">{s.scheduled_enabled && passphraseSet ? `Next ${whenLabel(nextRun(last?.created_at))}` : "Nightly backups off"}</span>,
              <span key="w">Saved to {where}</span>
            ]}
          />
        ) : (
          <Skeleton className="h-5 w-72 max-w-full" />
        )}

        <div className="mt-4">
          {list.isLoading ? (
            <div className="space-y-2">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-11 w-full" />)}</div>
          ) : !entries.length ? (
            <EmptyState icon={DatabaseBackup} title="No backups yet." />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-left text-[11px] font-semibold uppercase tracking-wider text-subtle">
                  <tr className="border-b border-border">
                    <th className="py-2 pr-3">Backup</th>
                    <th className="py-2 pr-3">Location</th>
                    <th className="hidden py-2 pr-3 text-right sm:table-cell">Size</th>
                    <th className="py-2" />
                  </tr>
                </thead>
                <tbody>
                  {entries.map((e) => (
                    <tr key={e.id} className="border-b border-border/50 last:border-0">
                      <td className="max-w-[16rem] py-2 pr-3">
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{e.when ? relativeTime(e.when) : "Unknown date"}</span>
                          {e.verified ? <Badge tone="success">Verified</Badge> : e.status && !["created", "verified", "completed"].includes(e.status) ? <Badge tone="warning">{e.status.replace(/_/g, " ")}</Badge> : null}
                        </div>
                        <div className="truncate text-xs text-subtle" title={e.name}>{e.name}</div>
                      </td>
                      <td className="whitespace-nowrap py-2 pr-3 text-muted">{locationLabel[e.location]}</td>
                      <td className="tabular hidden py-2 pr-3 text-right text-muted sm:table-cell">{e.size ? formatBytes(e.size) : "-"}</td>
                      <td className="py-2">
                        <div className="flex items-center justify-end gap-1">
                          <Button size="sm" variant="secondary" onClick={() => setRestore({ name: e.name, local: e.local, remoteKey: e.remoteKey })}>
                            <RotateCcw /> Restore
                          </Button>
                          {e.location === "cloud" && e.remoteKey && (
                            <RowMenu actions={[{ label: "Copy to this server", icon: CloudDownload, onSelect: () => void downloadToServer(e.remoteKey!) }]} />
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {cloudOn && remote.isError && <p className="mt-2 text-xs text-warning">Could not list cloud backups. Check the cloud settings below.</p>}
        </div>
      </Card>

      <Expander id="backup-settings" title="Settings" description="Recovery passphrase, schedule and cloud storage." open={settingsOpen} onOpenChange={setSettingsOpen}>
        <SettingsForm settings={s} />
      </Expander>

      <RestoreDialog target={restore} onClose={() => setRestore(null)} />
    </div>
  );
}

function RestoreDialog({ target, onClose }: { target: RestoreTarget | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [pass, setPass] = useState("");
  const [busy, setBusy] = useState(false);
  const close = () => {
    setPass("");
    onClose();
  };
  return (
    <Dialog open={!!target} onOpenChange={(v) => { if (!v) close(); }}>
      <DialogContent title="Restore backup">
        {target && (
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              try {
                if (target.local) {
                  const out = await api.post<{ requirements?: unknown }>(`/api/v1/admin/backups/${target.local.id}/restore`, { confirm: true, passphrase: pass });
                  if (out?.requirements) qc.setQueryData(["admin-backup-requirements"], out.requirements);
                } else if (target.remoteKey) {
                  await api.post("/api/v1/admin/backups/import-remote", { key: target.remoteKey, restore: true, confirm: true, passphrase: pass });
                }
                toast.success("Restore finished. Review the notes on this page, then wait for the restart.");
                void qc.invalidateQueries({ queryKey: ["admin-backups"] });
                void qc.invalidateQueries({ queryKey: ["admin-backup-requirements"] });
                close();
              } catch (err) {
                toast.error(errorMessage(err, "Restore failed"));
              } finally {
                setBusy(false);
              }
            }}
          >
            <p className="break-all text-sm text-muted">
              Replace everything on this server with <span className="font-medium text-foreground">{target.name}</span>. This cannot be undone.
            </p>
            <Field label="Recovery passphrase">
              <Input type="password" value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="off" required />
            </Field>
            <div className="flex gap-2">
              <Button type="submit" variant="destructive" disabled={busy || !pass}>{busy ? "Restoring…" : "Restore"}</Button>
              <Button type="button" variant="ghost" onClick={close}>Cancel</Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function SettingsForm({ settings }: { settings?: BackupSettings }) {
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
      void qc.invalidateQueries({ queryKey: settingsKey });
      void qc.invalidateQueries({ queryKey: ["admin-backup-remote"] });
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
    void qc.invalidateQueries({ queryKey: settingsKey });
  }

  return (
    <div className="space-y-6">
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.post("/api/v1/admin/backups/passphrase", { passphrase, current_passphrase: currentPass });
            setPassphrase("");
            setCurrentPass("");
            toast.success(passphraseSet ? "Passphrase changed" : "Recovery passphrase set");
            void qc.invalidateQueries({ queryKey: settingsKey });
          } catch (err) {
            toast.error(errorMessage(err, "Could not set passphrase"));
          }
        }}
      >
        <div>
          <div className="text-sm font-medium">Recovery passphrase</div>
          <p className="text-xs text-subtle">
            SoundDock never stores it; you need it to restore. Scheduled backups run without it.
            {passphraseSet ? " Changing it applies to new backups; older ones still need the old passphrase." : ""}
          </p>
        </div>
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
              <Button type="button" size="sm" variant="secondary" onClick={() => void downloadReminder()}>Download recovery reminder</Button>
              <Button type="button" size="sm" variant="ghost" onClick={async () => { await api.post("/api/v1/admin/backups/reminder/dismiss"); void qc.invalidateQueries({ queryKey: settingsKey }); }}>Skip reminder</Button>
            </>
          )}
        </div>
      </form>

      <div className="divide-y divide-border border-t border-border pt-2">
        <SettingSwitch
          label="Back up every night"
          description={passphraseSet ? undefined : "Needs a recovery passphrase first."}
          checked={!!st.scheduled_enabled}
          disabled={!passphraseSet}
          onChange={(v) => setForm({ ...form, scheduled_enabled: v })}
        />
        <SettingSwitch label="Include downloaded music" description="Turn off for a smaller, database only backup." checked={!!st.include_media} onChange={(v) => setForm({ ...form, include_media: v })} />
        <SettingSwitch label="Keep a copy on this server" checked={!!st.local_enabled} onChange={(v) => setForm({ ...form, local_enabled: v })} />
        <SettingSwitch label="Copy to Cloudflare R2 or S3" checked={!!st.r2_enabled} onChange={(v) => setForm({ ...form, r2_enabled: v })} />
      </div>
      {st.r2_enabled && (
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Endpoint"><Input value={st.endpoint || ""} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} placeholder="xxx.r2.cloudflarestorage.com" /></Field>
          <Field label="Bucket"><Input value={st.bucket || ""} onChange={(e) => setForm({ ...form, bucket: e.target.value })} /></Field>
          <Field label="Access key"><Input value={st.access_key || ""} onChange={(e) => setForm({ ...form, access_key: e.target.value })} /></Field>
          <Field label="Secret key" hint={st.secret_set ? "Saved. Leave blank to keep it." : undefined}><Input type="password" value={form.secret_key || ""} onChange={(e) => setForm({ ...form, secret_key: e.target.value })} /></Field>
          <Field label="Folder in bucket"><Input value={st.prefix || ""} onChange={(e) => setForm({ ...form, prefix: e.target.value })} placeholder="sounddock-backups" /></Field>
          <Field label="Region"><Input value={st.region || "auto"} onChange={(e) => setForm({ ...form, region: e.target.value })} /></Field>
        </div>
      )}
      <SaveBar dirty={dirty} saving={saving} onSave={() => void saveSettings()} onReset={() => setForm({})} />
    </div>
  );
}
