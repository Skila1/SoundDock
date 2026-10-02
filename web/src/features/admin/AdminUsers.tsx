import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Gauge, Search } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge, Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { formatBytes, relativeTime } from "@/lib/utils";
import type { User } from "@/types/api";
import { ByteInput, RowMenu, SettingSwitch, errorMessage } from "./adminUi";
import { capLabel } from "./adminFormat";
import { fetchQuotas, overrideFor, quotasKey, saveQuotas } from "./adminQuotas";

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

const roleOptions = [
  { value: "User", label: "User" },
  { value: "Administrator", label: "Administrator" }
];

export function AdminUsers() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const q = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<AdminUserRow[]>("/api/v1/admin/users") });
  const quotas = useQuery({ queryKey: quotasKey, queryFn: fetchQuotas });
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<AdminUserRow | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [limitsOpen, setLimitsOpen] = useState(false);
  const [discordOpen, setDiscordOpen] = useState(false);
  const rows = (q.data || []).filter((u) =>
    `${u.username} ${u.display_name || ""} ${u.email || ""} ${u.discord_id || ""} ${u.discord_username || ""} ${u.role || ""}`.toLowerCase().includes(filter.toLowerCase())
  );
  const defaultCap = quotas.data?.default_user_bytes || 0;
  const usage = quotas.data?.user_usage || {};
  const capFor = (id: string) => overrideFor(quotas.data?.users, (r) => r.user_id === id) ?? defaultCap;

  return (
    <div>
      <PageHeader
        title="Users"
        description="Click someone to change their access, upload limit, or Discord link."
        actions={
          <div className="flex items-center gap-2">
            <Button onClick={() => setCreateOpen(true)}>Create user</Button>
            <RowMenu
              label="More user actions"
              actions={[
                { label: "Default upload limit", icon: Gauge, onSelect: () => setLimitsOpen(true) },
                { label: "Look up a Discord-only listener", icon: Search, onSelect: () => setDiscordOpen(true) }
              ]}
            />
          </div>
        }
      />
      <Input className="mb-4 max-w-sm" placeholder="Search users" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Search users" />
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted">
            <tr><th className="p-3">User</th><th className="p-3">Discord</th><th className="p-3">Access</th><th className="p-3">Uploads</th><th className="p-3">Status</th><th className="p-3">Joined</th></tr>
          </thead>
          <tbody>
            {rows.map((u) => {
              const cap = capFor(u.id);
              const used = usage[u.id] || 0;
              return (
                <tr key={u.id} className="cursor-pointer border-t border-border hover:bg-surface-2/60" onClick={() => setSelected(u)}>
                  <td className="p-3">
                    <div className="font-medium">{u.display_name || u.username}</div>
                    <div className="text-xs text-subtle">{u.username}{u.email ? ` · ${u.email}` : ""}</div>
                  </td>
                  <td className="p-3">{u.discord_username || u.discord_id || <span className="text-subtle">-</span>}</td>
                  <td className="p-3"><Badge tone={u.role === "Administrator" ? "success" : "neutral"}>{u.role || "User"}</Badge></td>
                  <td className="w-40 p-3">
                    <div className="text-xs text-muted">{formatBytes(used)}{cap ? ` of ${capLabel(cap)}` : ""}</div>
                    {cap > 0 && <Progress value={(used / cap) * 100} />}
                  </td>
                  <td className="p-3"><Badge tone={u.disabled ? "danger" : "success"}>{u.disabled ? "Disabled" : "Active"}</Badge></td>
                  <td className="p-3 text-muted">{relativeTime(u.created_at)}</td>
                </tr>
              );
            })}
            {!rows.length && <tr><td className="p-6 text-center text-muted" colSpan={6}>{q.isLoading ? "Loading…" : "No users match."}</td></tr>}
          </tbody>
        </table>
      </div>

      <ManageUserDialog
        user={selected}
        selfId={me.data?.id}
        override={selected ? overrideFor(quotas.data?.users, (r) => r.user_id === selected.id) : null}
        defaultCap={defaultCap}
        used={selected ? usage[selected.id] || 0 : 0}
        onClose={() => setSelected(null)}
        onChanged={async () => {
          await qc.invalidateQueries({ queryKey: ["admin-users"] });
          await qc.invalidateQueries({ queryKey: quotasKey });
        }}
      />
      <CreateUserDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={() => qc.invalidateQueries({ queryKey: ["admin-users"] })} />
      <DefaultLimitDialog open={limitsOpen} onOpenChange={setLimitsOpen} value={defaultCap} onSaved={() => qc.invalidateQueries({ queryKey: quotasKey })} />
      <Dialog open={discordOpen} onOpenChange={setDiscordOpen}>
        <DialogContent title="Discord-only listener">
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              const id = String(new FormData(e.currentTarget).get("discord") || "").trim();
              if (id) navigate(`/admin/discord-users/${encodeURIComponent(id)}/library`);
            }}
          >
            <p className="text-sm text-muted">People who only use the Discord bot have no SoundDock account. Enter their Discord user ID to see their library.</p>
            <Field label="Discord user ID"><Input name="discord" inputMode="numeric" placeholder="123456789012345678" required /></Field>
            <Button type="submit">Open library</Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function CreateUserDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (v: boolean) => void; onCreated: () => void }) {
  const [form, setForm] = useState({ username: "", password: "", role: "User" });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Create user">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await api.post("/api/v1/admin/users", form);
              toast.success("User created");
              setForm({ username: "", password: "", role: "User" });
              onOpenChange(false);
              onCreated();
            } catch (err) {
              toast.error(errorMessage(err, "Could not create user"));
            }
          }}
        >
          <Field label="Username"><Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required autoComplete="off" /></Field>
          <Field label="Password"><Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} required autoComplete="new-password" /></Field>
          <Field label="Role"><Select value={form.role} onValueChange={(role) => setForm({ ...form, role })} options={roleOptions} /></Field>
          <Button type="submit">Create</Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DefaultLimitDialog({ open, onOpenChange, value, onSaved }: { open: boolean; onOpenChange: (v: boolean) => void; value: number; onSaved: () => void }) {
  const [bytes, setBytes] = useState<number | null>(value);
  useEffect(() => { if (open) setBytes(value); }, [open, value]);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Default upload limit">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await saveQuotas({ defaultUserBytes: bytes || 0 });
              toast.success("Default upload limit saved");
              onOpenChange(false);
              onSaved();
            } catch (err) {
              toast.error(errorMessage(err, "Could not save limit"));
            }
          }}
        >
          <p className="text-sm text-muted">How much each person may upload unless they have their own limit. 0 means unlimited.</p>
          <ByteInput value={bytes} onChange={setBytes} />
          <Button type="submit">Save</Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ManageUserDialog({
  user,
  selfId,
  override,
  defaultCap,
  used,
  onClose,
  onChanged
}: {
  user: AdminUserRow | null;
  selfId?: string;
  override: number | null;
  defaultCap: number;
  used: number;
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const navigate = useNavigate();
  const [role, setRole] = useState("User");
  const [disabled, setDisabled] = useState(false);
  const [limitMode, setLimitMode] = useState<"default" | "custom">("default");
  const [limit, setLimit] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  useEffect(() => {
    setConfirmDelete(false);
    if (!user) return;
    setRole(user.role || "User");
    setDisabled(!!user.disabled);
    setLimitMode(override == null ? "default" : "custom");
    setLimit(override);
  }, [user, override]);
  const isSelf = !!user && user.id === selfId;
  const accessDirty = !!user && (role !== (user.role || "User") || disabled !== !!user.disabled);
  const nextOverride = limitMode === "default" ? null : limit ?? 0;
  const limitDirty = nextOverride !== override;

  async function run(fn: () => Promise<void>, ok: string, fail: string) {
    setBusy(true);
    try {
      await fn();
      toast.success(ok);
      await onChanged();
      return true;
    } catch (e) {
      toast.error(errorMessage(e, fail));
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    if (!user) return;
    const ok = await run(async () => {
      if (accessDirty) await api.patch(`/api/v1/admin/users/${user.id}`, { role, disabled });
      if (limitDirty) await saveQuotas({ user: { id: user.id, bytes: nextOverride } });
    }, "User saved", "Could not save user");
    if (ok) onClose();
  }

  return (
    <Dialog open={!!user} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title={user ? user.display_name || user.username : "User"} className="max-h-[90vh] overflow-y-auto">
        {user && (
          <div className="space-y-4">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              <dt className="text-muted">Username</dt><dd className="font-medium">{user.username}</dd>
              <dt className="text-muted">Email</dt><dd>{user.email || "-"}</dd>
              <dt className="text-muted">Discord</dt><dd>{user.discord_username || user.discord_id || "Not linked"}</dd>
              <dt className="text-muted">Groups</dt><dd>{(user.roles || []).join(", ") || "-"}</dd>
              <dt className="text-muted">Joined</dt><dd>{relativeTime(user.created_at)}</dd>
            </dl>
            <Field label="Role" hint="Custom groups are managed under Groups and stay assigned.">
              <Select value={role} onValueChange={setRole} options={roleOptions} />
            </Field>
            <SettingSwitch
              label="Disabled"
              description={isSelf ? "You cannot disable your own account." : "Blocks sign-in and ends their sessions."}
              checked={disabled}
              disabled={isSelf}
              onChange={setDisabled}
            />
            <div>
              <div className="mb-1 text-sm font-medium">Upload limit</div>
              <p className="mb-2 text-xs text-subtle">Uploaded {formatBytes(used)}. Default is {capLabel(defaultCap)}.</p>
              <div className="flex flex-wrap items-center gap-2">
                <Select className="w-44" value={limitMode} onValueChange={(v) => setLimitMode(v as "default" | "custom")} options={[{ value: "default", label: "Use default" }, { value: "custom", label: "Custom limit" }]} />
                {limitMode === "custom" && <ByteInput value={limit} onChange={setLimit} placeholder="0 = unlimited" />}
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2 border-t border-border pt-4">
              <Button type="button" disabled={busy || (!accessDirty && !limitDirty)} onClick={save}>Save</Button>
              <Button type="button" variant="secondary" onClick={() => navigate(`/admin/users/${user.id}/library`)}>Open library</Button>
              <div className="ml-auto">
                <RowMenu
                  actions={[
                    ...(user.discord_id
                      ? [{ label: "Unlink Discord", onSelect: () => void run(() => api.del(`/api/v1/admin/users/${user.id}/identities/discord`).then(() => undefined), "Discord unlinked", "Could not unlink Discord").then((ok) => { if (ok) onClose(); }) }]
                      : []),
                    { label: "Delete user", destructive: true, disabled: isSelf, separatorBefore: !!user.discord_id, onSelect: () => setConfirmDelete(true) }
                  ]}
                />
              </div>
            </div>
            {confirmDelete && (
              <div className="space-y-2 rounded-lg border border-destructive/40 p-3">
                <p className="text-sm text-muted">This removes the account, sessions, playlists, and Discord link. It cannot be undone.</p>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="destructive"
                    disabled={busy}
                    onClick={async () => {
                      const ok = await run(() => api.del(`/api/v1/admin/users/${user.id}`).then(() => undefined), "User deleted", "Could not delete user");
                      if (ok) onClose();
                    }}
                  >
                    Delete forever
                  </Button>
                  <Button type="button" variant="ghost" disabled={busy} onClick={() => setConfirmDelete(false)}>Cancel</Button>
                </div>
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
