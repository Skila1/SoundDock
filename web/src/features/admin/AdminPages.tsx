import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { relativeTime } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";

export function AdminRoles() {
  return (
    <div>
      <PageHeader title="Roles" description="Built-in roles control administration and library grants." />
      <div className="grid gap-3 md:grid-cols-2">
        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <h3 className="font-semibold">Administrator</h3>
          <p className="mt-1 text-sm text-muted">Full library grants, user management, storage, jobs, backups, and integrations.</p>
        </article>
        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <h3 className="font-semibold">User</h3>
          <p className="mt-1 text-sm text-muted">Read and stream granted libraries. Upload and import into writable libraries.</p>
        </article>
      </div>
      <p className="mt-4 text-sm text-muted">Assign a role when creating a user. Library grants are applied automatically for Administrator and User.</p>
    </div>
  );
}

export function AdminUsers() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<any[]>("/api/v1/admin/users") });
  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ username: "", password: "", role: "User" });
  const rows = (q.data || []).filter((u) => `${u.username} ${u.display_name || ""} ${u.email || ""} ${u.discord_id || ""} ${u.discord_username || ""}`.toLowerCase().includes(filter.toLowerCase()));
  return (
    <div>
      <PageHeader title="Users" actions={<Button onClick={() => setOpen(true)}>Create user</Button>} />
      <Input className="mb-4 max-w-sm" placeholder="Search users" value={filter} onChange={(e) => setFilter(e.target.value)} />
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted"><tr><th className="p-3">User</th><th className="p-3">Discord</th><th className="p-3">Status</th><th className="p-3">Created</th><th className="p-3" /></tr></thead>
          <tbody>
            {rows.map((u) => (
              <tr key={u.id} className="border-t border-border">
                <td className="p-3">
                  <div className="font-medium">{u.display_name || u.username}</div>
                  <div className="text-xs text-subtle">{u.username}{u.email ? ` · ${u.email}` : ""}</div>
                </td>
                <td className="p-3">
                  {u.discord_id ? (
                    <>
                      <div className="font-medium">{u.discord_username || "—"}</div>
                      <div className="text-xs text-subtle">{u.discord_id}</div>
                    </>
                  ) : (
                    <span className="text-subtle">—</span>
                  )}
                </td>
                <td className="p-3"><Badge tone={u.disabled ? "danger" : "success"}>{u.disabled ? "Disabled" : "Active"}</Badge></td>
                <td className="p-3 text-muted">{relativeTime(u.created_at)}</td>
                <td className="p-3 text-right">
                  <Button size="sm" variant="ghost" onClick={() => api.patch(`/api/v1/admin/users/${u.id}`, { disabled: !u.disabled }).then(() => { toast.success(u.disabled ? "Enabled" : "Disabled"); qc.invalidateQueries({ queryKey: ["admin-users"] }); })}>
                    {u.disabled ? "Enable" : "Disable"}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
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

export function AdminStorage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-storage"], queryFn: () => api.get<any[]>("/api/v1/admin/storage") });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: "", type: "local", root: "" });
  return (
    <div>
      <PageHeader title="Storage" actions={<Button onClick={() => setOpen(true)}>Add provider</Button>} />
      <div className="grid gap-3 md:grid-cols-2">
        {(q.data || []).map((s) => (
          <article key={s.id} className="rounded-xl border border-border bg-surface-1 p-4">
            <h3 className="font-semibold">{s.name}</h3>
            <p className="text-sm capitalize text-muted">{s.type}</p>
            <Badge className="mt-2" tone="success">Configured</Badge>
          </article>
        ))}
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="Add storage">
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            await api.post("/api/v1/admin/storage", { name: form.name, type: form.type, config: { root: form.root } });
            toast.success("Storage added");
            setOpen(false);
            qc.invalidateQueries({ queryKey: ["admin-storage"] });
          }}>
            <Field label="Name"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required /></Field>
            <Field label="Type"><Select value={form.type} onValueChange={(type) => setForm({ ...form, type })} options={[{ value: "local", label: "Local" }, { value: "managed", label: "Managed" }, { value: "s3", label: "S3" }]} /></Field>
            <Field label="Root / endpoint" hint="Secrets are write-only and never shown again."><Input value={form.root} onChange={(e) => setForm({ ...form, root: e.target.value })} /></Field>
            <Button type="submit">Save</Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export function AdminLibraries() {
  const qc = useQueryClient();
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<any[]>("/api/v1/libraries") });
  const storage = useQuery({ queryKey: ["admin-storage"], queryFn: () => api.get<any[]>("/api/v1/admin/storage") });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: "", storage_id: "", kind: "music", organisation_mode: "virtual" });
  return (
    <div>
      <PageHeader title="Libraries" actions={<Button onClick={() => setOpen(true)}>Create library</Button>} />
      <div className="space-y-3">
        {(libs.data || []).map((l) => (
          <div key={l.id} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-surface-1 p-4">
            <div>
              <div className="font-semibold">{l.name}</div>
              <div className="text-xs text-muted">{l.kind} · {l.organisation_mode} · {l.track_count ?? 0} tracks</div>
            </div>
            <Button size="sm" onClick={() => api.post(`/api/v1/admin/libraries/${l.id}/scan`).then(() => toast.success("Scan started"))}>Scan</Button>
          </div>
        ))}
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="Create library">
          <form className="space-y-3" onSubmit={async (e) => {
            e.preventDefault();
            await api.post("/api/v1/admin/libraries", { name: form.name, kind: form.kind, storage_id: form.storage_id, Org: form.organisation_mode });
            toast.success("Library created");
            setOpen(false);
            qc.invalidateQueries({ queryKey: ["libraries"] });
          }}>
            <Field label="Name"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required /></Field>
            <Field label="Storage"><Select value={form.storage_id} onValueChange={(storage_id) => setForm({ ...form, storage_id })} options={(storage.data || []).map((s: any) => ({ value: s.id, label: s.name }))} /></Field>
            <Button type="submit">Create</Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export function AdminJobs() {
  const q = useQuery({ queryKey: ["admin-jobs"], queryFn: () => api.get<any[]>("/api/v1/admin/jobs"), refetchInterval: 4000 });
  return (
    <div>
      <PageHeader title="Jobs" />
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted"><tr><th className="p-3">Type</th><th className="p-3">State</th><th className="p-3">Progress</th><th className="p-3">Started</th><th className="p-3">Error</th><th /></tr></thead>
          <tbody>
            {(q.data || []).map((j) => (
              <tr key={j.id} className="border-t border-border">
                <td className="p-3">{j.type}</td>
                <td className="p-3"><Badge tone={j.status === "failed" ? "danger" : j.status === "succeeded" ? "success" : "accent"}>{j.status}</Badge></td>
                <td className="p-3 w-32"><div className="h-1.5 rounded-full bg-surface-3"><div className="h-full rounded-full bg-accent" style={{ width: `${j.progress || 0}%` }} /></div></td>
                <td className="p-3 text-muted">{relativeTime(j.created_at)}</td>
                <td className="max-w-xs truncate p-3 text-destructive">{j.last_error}</td>
                <td className="p-3"><Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/jobs/${j.id}/cancel`).then(() => toast("Cancel requested"))}>Cancel</Button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export function AdminBackups() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-backups"], queryFn: () => api.get<any[]>("/api/v1/admin/backups") });
  const last = q.data?.[0];
  return (
    <div>
      <PageHeader title="Backups" actions={<Button onClick={() => api.post("/api/v1/admin/backups").then(() => { toast.success("Backup started"); qc.invalidateQueries({ queryKey: ["admin-backups"] }); })}>Run backup</Button>} />
      {last && (
        <div className="mb-4 rounded-xl border border-border bg-surface-1 p-4">
          <div className="text-sm text-muted">Last backup</div>
          <div className="font-medium">{last.path}</div>
          <div className="mt-1 flex gap-2 text-sm">
            <Badge tone={last.verified ? "success" : "warning"}>{last.verified ? "Verified" : "Unverified"}</Badge>
            <Badge>{last.status}</Badge>
          </div>
        </div>
      )}
      <ul className="space-y-2">
        {(q.data || []).map((b) => (
          <li key={b.id} className="rounded-lg border border-border px-4 py-3 text-sm">
            {b.path} · {relativeTime(b.created_at)}
          </li>
        ))}
      </ul>
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
      <PageHeader title="Discord" description="Optional. Sign-in, bot, and who may register. Nothing here lives in .env." />
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
      <h3 className="mb-2 font-semibold">Guilds</h3>
      {!guilds.data?.length && <p className="mb-4 text-sm text-muted">Invite the SoundDock bot to get started.</p>}
      <ul className="space-y-2">
        {(guilds.data || []).map((g) => (
          <li key={g.id} className="flex items-center justify-between rounded-lg border border-border px-4 py-3">
            <div>
              <div className="font-medium">{g.name || g.id}</div>
              <div className="text-xs text-muted">volume {g.default_volume} · queue {g.queue_limit}</div>
            </div>
            <Button size="sm" variant="ghost" onClick={() => api.post(`/api/v1/admin/integrations/discord/guilds/${g.id}/disconnect`).then(() => toast("Disconnected"))}>Disconnect voice</Button>
          </li>
        ))}
      </ul>
      <h3 className="mb-2 mt-6 font-semibold">Voice sessions</h3>
      {(sessions.data || []).map((s) => (
        <div key={s.guild_id} className="text-sm text-muted">{s.guild_id} · {s.connected ? "connected" : s.reason || "idle"}</div>
      ))}
    </div>
  );
}

export function AdminIntegrations() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-int"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations") });
  const [name, setName] = useState("");
  const [secret, setSecret] = useState("");
  return (
    <div>
      <PageHeader title="Integrations" description="API clients for bots and automations. Secrets are shown once." />
      <form className="mb-4 flex max-w-lg gap-2" onSubmit={async (e) => {
        e.preventDefault();
        const r = await api.post<any>("/api/v1/admin/integrations", { name, scopes: ["read", "stream"] });
        setSecret(r.secret);
        toast.success("Client created");
        qc.invalidateQueries({ queryKey: ["admin-int"] });
      }}>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Client name" />
        <Button type="submit">Create</Button>
      </form>
      {secret && <p className="mb-4 rounded-lg bg-surface-2 p-3 text-sm">Copy this key now: <code>{secret}</code></p>}
      <ul className="space-y-2">
        {(q.data || []).map((c) => (
          <li key={c.id} className="flex items-center justify-between rounded-lg border border-border px-4 py-3">
            <div><div className="font-medium">{c.name}</div><div className="text-xs text-muted">{(c.scopes || []).join(", ")}</div></div>
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
      <PageHeader title="External playlist providers" description="OAuth and MusicKit credentials are write-only. SoundDock imports playlist metadata only." />
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

export function AdminMetadata() {
  const q = useQuery({ queryKey: ["admin-meta"], queryFn: () => api.get<any>("/api/v1/admin/metadata") });
  return (
    <div>
      <PageHeader title="Metadata" description="MusicBrainz and Cover Art Archive lookups." />
      <div className="flex items-center gap-3 rounded-xl border border-border bg-surface-1 p-4">
        <Switch checked={!!q.data?.external_enabled} onCheckedChange={(v) => api.put("/api/v1/admin/metadata", { external_enabled: v }).then(() => { toast.success("Settings saved"); q.refetch(); })} />
        <div>
          <div className="font-medium">External providers</div>
          <div className="text-sm text-muted">{(q.data?.providers || []).join(", ") || "musicbrainz, coverartarchive"}</div>
        </div>
      </div>
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

export function AdminRetention() {
  const q = useQuery({ queryKey: ["ret"], queryFn: () => api.get<any[]>("/api/v1/admin/retention") });
  const [days, setDays] = useState<Record<string, string>>({});
  return (
    <div>
      <PageHeader title="Retention" />
      <form className="max-w-md space-y-3" onSubmit={async (e) => {
        e.preventDefault();
        const body: Record<string, number> = {};
        (q.data || []).forEach((r) => { body[r.key] = Number(days[r.key] ?? r.days); });
        await api.put("/api/v1/admin/retention", body);
        toast.success("Retention saved");
      }}>
        {(q.data || []).map((r) => (
          <Field key={r.key} label={r.key}>
            <Input type="number" value={days[r.key] ?? r.days} onChange={(e) => setDays({ ...days, [r.key]: e.target.value })} />
          </Field>
        ))}
        <Button type="submit">Save</Button>
      </form>
    </div>
  );
}

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
  const q = useQuery({ queryKey: ["logs"], queryFn: () => api.get<any[]>("/api/v1/admin/logs") });
  return (
    <div>
      <PageHeader title="Logs" description="Recent job errors." />
      <ul className="space-y-2">
        {(q.data || []).map((l, i) => (
          <li key={i} className="rounded-lg border border-border px-4 py-3 text-sm">
            <div className="font-medium">{l.type}</div>
            <div className="text-destructive">{l.error}</div>
            <div className="text-xs text-subtle">{relativeTime(l.at)}</div>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function AdminUpdates() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-updates"], queryFn: () => api.get<any>("/api/v1/admin/updates"), refetchInterval: 8000 });
  const d = q.data || {};
  const [busy, setBusy] = useState(false);
  return (
    <div>
      <PageHeader title="Updates" description="Pull the latest SoundDock image and restart the stack from here." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.available ? "warning" : "success"}>{d.available ? "Update available" : "Up to date"}</Badge>
        <Badge tone={d.socket_ok ? "success" : "warning"}>{d.socket_ok ? "Docker ready" : "Docker socket missing"}</Badge>
        <Badge>{d.last_status || "idle"}</Badge>
      </div>
      <div className="mb-4 rounded-xl border border-border bg-surface-1 p-5">
        <div className="text-sm text-muted">Installed version</div>
        <div className="text-2xl font-semibold">{d.version || "dev"}</div>
        <p className="mt-1 break-all text-xs text-subtle">{d.image}</p>
        <p className="mt-3 text-sm text-muted">Last check: {d.last_check_at ? relativeTime(d.last_check_at) : "never"}</p>
        <p className="text-sm text-muted">Last update: {d.last_applied_at ? relativeTime(d.last_applied_at) : "never"}{d.last_applied_by ? ` (${d.last_applied_by})` : ""}</p>
        {d.last_error && <p className="mt-2 text-sm text-destructive">{d.last_error}</p>}
        {!d.socket_ok && <p className="mt-2 text-sm text-muted">Re-run the installer so the Docker socket is mounted. Checking for a newer image still works.</p>}
        <div className="mt-4 flex flex-wrap gap-2">
          <Button variant="secondary" disabled={busy || d.checking} onClick={async () => {
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
          <Button disabled={busy || d.updating || !d.can_apply} onClick={async () => {
            setBusy(true);
            try {
              await api.post("/api/v1/admin/updates/apply");
              toast.success("Update started. This page will reconnect after restart.");
              const started = Date.now();
              const tick = setInterval(async () => {
                try {
                  await fetch("/healthz", { cache: "no-store" });
                  if (Date.now() - started > 8000) {
                    clearInterval(tick);
                    location.reload();
                  }
                } catch {
                  /* down during restart */
                }
              }, 2000);
              setTimeout(() => { clearInterval(tick); location.reload(); }, 120000);
            } catch (e: any) {
              toast.error(e?.message || "Could not start update");
            } finally {
              setBusy(false);
            }
          }}>{d.updating ? "Updating…" : "Update now"}</Button>
        </div>
      </div>
      <div className="flex max-w-lg items-center justify-between rounded-xl border border-border bg-surface-1 p-4">
        <div>
          <div className="text-sm font-medium">Automatic updates</div>
          <p className="text-xs text-subtle">When on, SoundDock checks about once an hour and applies a newer image when Docker is available.</p>
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
