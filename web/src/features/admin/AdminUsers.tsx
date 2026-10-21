import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Library, MessageCircle, Search, Trash2, Unlink, UserPlus } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge, Progress } from "@/components/ui/misc";
import { PageHeader, QueryError } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { formatBytes, relativeTime } from "@/lib/utils";
import type { User } from "@/types/api";
import { ByteInput, SettingSwitch, errorMessage } from "./adminUi";
import { capLabel } from "./adminFormat";
import { fetchQuotas, overrideFor, quotasKey, saveQuotas } from "./adminQuotas";
import { Avatar, Chip, Drawer, DrawerSection, groupsKey, useAdminUsers, useGroups, userLabel, usersKey, type AdminUserRow, type GroupRow } from "./iam/iamShared";

const DISCORD_ID = /^\d{15,22}$/;

export function AdminUsers() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const users = useAdminUsers();
  const groups = useGroups();
  const quotas = useQuery({ queryKey: quotasKey, queryFn: fetchQuotas });
  const [filter, setFilter] = useState("");
  const [groupFilter, setGroupFilter] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const defaultCap = quotas.data?.default_user_bytes || 0;
  const usage = quotas.data?.user_usage || {};
  const capFor = (id: string) => overrideFor(quotas.data?.users, (r) => r.user_id === id) ?? defaultCap;
  const query = filter.trim();
  const discordLookup = DISCORD_ID.test(query) ? query : "";

  const rows = useMemo(() => {
    const q = query.toLowerCase();
    return (users.data || []).filter((u) => {
      if (groupFilter === "disabled" && !u.disabled) return false;
      if (groupFilter !== "all" && groupFilter !== "disabled" && !(u.roles || []).includes(groupFilter)) return false;
      if (!q) return true;
      return `${u.username} ${u.display_name || ""} ${u.email || ""} ${u.discord_id || ""} ${u.discord_username || ""}`.toLowerCase().includes(q);
    });
  }, [users.data, query, groupFilter]);

  const selected = (users.data || []).find((u) => u.id === selectedId) || null;
  const filterOptions = [
    { value: "all", label: "Everyone" },
    ...(groups.data || []).map((g) => ({ value: g.name, label: g.name })),
    { value: "disabled", label: "Disabled accounts" }
  ];
  const refresh = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: usersKey }),
      qc.invalidateQueries({ queryKey: groupsKey }),
      qc.invalidateQueries({ queryKey: quotasKey })
    ]);
  };

  return (
    <div>
      <PageHeader
        title="Users"
        description="Everyone who can sign in. Click a person to change their groups, upload limit or account."
        actions={<Button onClick={() => setCreateOpen(true)}><UserPlus /> Add user</Button>}
      />

      <div className="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative min-w-0 flex-1 sm:max-w-md">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-subtle" />
          <Input
            className="pl-9"
            placeholder="Search people, or paste a Discord ID"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            aria-label="Search people or look up a Discord listener"
          />
        </div>
        <Select className="sm:w-48" value={groupFilter} onValueChange={setGroupFilter} options={filterOptions} />
      </div>

      {discordLookup && (
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-accent/30 bg-accent/5 px-4 py-3">
          <div className="flex min-w-0 items-center gap-3">
            <MessageCircle className="h-4 w-4 shrink-0 text-accent" />
            <p className="min-w-0 text-sm">
              <span className="font-medium">Discord listener {discordLookup}</span>
              <span className="block text-xs text-muted">People who only use the bot have no account. You can still see what they play.</span>
            </p>
          </div>
          <Button size="sm" variant="secondary" onClick={() => navigate(`/admin/discord-users/${encodeURIComponent(discordLookup)}/library`)}>
            <Library /> Open their library
          </Button>
        </div>
      )}

      {users.isError ? (
        <QueryError message={errorMessage(users.error, "Could not load users")} onRetry={() => users.refetch()} />
      ) : (
        <div className="overflow-hidden rounded-2xl border border-border bg-surface-1 shadow-sm">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-border bg-surface-2/60 text-xs uppercase tracking-wider text-subtle">
              <tr>
                <th className="px-4 py-3 font-semibold">Person</th>
                <th className="hidden px-4 py-3 font-semibold lg:table-cell">Discord</th>
                <th className="hidden px-4 py-3 font-semibold md:table-cell">Groups</th>
                <th className="hidden px-4 py-3 font-semibold md:table-cell">Uploads</th>
                <th className="px-4 py-3 font-semibold">Status</th>
                <th className="hidden px-4 py-3 font-semibold lg:table-cell">Joined</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((u) => {
                const cap = capFor(u.id);
                const used = usage[u.id] || 0;
                return (
                  <tr
                    key={u.id}
                    tabIndex={0}
                    className="cursor-pointer border-t border-border first:border-t-0 hover:bg-surface-2/60 focus-visible:bg-surface-2/60 focus-visible:outline-none"
                    onClick={() => setSelectedId(u.id)}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setSelectedId(u.id); } }}
                  >
                    <td className="px-4 py-3">
                      <div className="flex min-w-0 items-center gap-3">
                        <Avatar name={userLabel(u)} />
                        <div className="min-w-0">
                          <div className="truncate font-medium">{userLabel(u)}{u.id === me.data?.id && <span className="ml-1.5 text-xs font-normal text-subtle">(you)</span>}</div>
                          <div className="truncate text-xs text-subtle">{u.username}{u.email ? ` · ${u.email}` : ""}</div>
                          <div className="mt-1 flex flex-wrap gap-1 md:hidden">
                            {(u.roles || []).map((r) => <GroupBadge key={r} name={r} />)}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td className="hidden px-4 py-3 text-muted lg:table-cell">{u.discord_username || u.discord_id || <span className="text-subtle">-</span>}</td>
                    <td className="hidden px-4 py-3 md:table-cell">
                      <div className="flex flex-wrap gap-1">
                        {(u.roles || []).length ? (u.roles || []).map((r) => <GroupBadge key={r} name={r} />) : <span className="text-subtle">-</span>}
                      </div>
                    </td>
                    <td className="hidden w-44 px-4 py-3 md:table-cell">
                      <div className="text-xs text-muted">{formatBytes(used)} of {capLabel(cap)}</div>
                      {cap > 0 && <div className="mt-1"><Progress value={(used / cap) * 100} /></div>}
                    </td>
                    <td className="px-4 py-3"><Badge tone={u.disabled ? "danger" : "success"}>{u.disabled ? "Disabled" : "Active"}</Badge></td>
                    <td className="hidden whitespace-nowrap px-4 py-3 text-muted lg:table-cell">{relativeTime(u.created_at)}</td>
                  </tr>
                );
              })}
              {!rows.length && (
                <tr>
                  <td className="px-4 py-10 text-center text-sm text-muted" colSpan={6}>{users.isLoading ? "Loading..." : "Nobody matches."}</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      <UserDrawer
        user={selected}
        selfId={me.data?.id}
        groups={groups.data || []}
        override={selected ? overrideFor(quotas.data?.users, (r) => r.user_id === selected.id) : null}
        defaultCap={defaultCap}
        used={selected ? usage[selected.id] || 0 : 0}
        onClose={() => setSelectedId(null)}
        onChanged={refresh}
      />
      <CreateUserDialog open={createOpen} onOpenChange={setCreateOpen} groups={groups.data || []} onCreated={refresh} />
    </div>
  );
}

function GroupBadge({ name }: { name: string }) {
  return <Badge tone={name === "Administrator" ? "accent" : "neutral"}>{name}</Badge>;
}

function CreateUserDialog({ open, onOpenChange, groups, onCreated }: { open: boolean; onOpenChange: (v: boolean) => void; groups: GroupRow[]; onCreated: () => void }) {
  const [form, setForm] = useState({ username: "", password: "", role: "User" });
  const [busy, setBusy] = useState(false);
  const options = groups.length ? groups.map((g) => ({ value: g.name, label: g.name })) : [{ value: "User", label: "User" }];
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Add user">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            try {
              await api.post("/api/v1/admin/users", form);
              toast.success(`${form.username} added`);
              setForm({ username: "", password: "", role: "User" });
              onOpenChange(false);
              onCreated();
            } catch (err) {
              toast.error(errorMessage(err, "Could not add user"));
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label="Username"><Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required autoComplete="off" /></Field>
          <Field label="Password"><Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} required autoComplete="new-password" /></Field>
          <Field label="First group" hint="You can add more groups after."><Select value={form.role} onValueChange={(role) => setForm({ ...form, role })} options={options} /></Field>
          <div className="flex justify-end">
            <Button type="submit" disabled={busy}>{busy ? "Adding..." : "Add user"}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function UserDrawer({
  user,
  selfId,
  groups,
  override,
  defaultCap,
  used,
  onClose,
  onChanged
}: {
  user: AdminUserRow | null;
  selfId?: string;
  groups: GroupRow[];
  override: number | null;
  defaultCap: number;
  used: number;
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const navigate = useNavigate();
  const [limitMode, setLimitMode] = useState<"default" | "custom">("default");
  const [limit, setLimit] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmUnlink, setConfirmUnlink] = useState(false);
  const userId = user?.id;
  useEffect(() => {
    setLimitMode(override == null ? "default" : "custom");
    setLimit(override);
  }, [userId, override]);

  const isSelf = !!user && user.id === selfId;
  const nextOverride = limitMode === "default" ? null : limit ?? 0;
  const limitDirty = nextOverride !== override;
  const memberOf = new Set(user?.roles || []);
  const addable = groups.filter((g) => !memberOf.has(g.name));
  const effectiveCap = override ?? defaultCap;

  async function run(fn: () => Promise<unknown>, ok: string, fail: string) {
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

  const name = user ? userLabel(user) : "User";
  return (
    <>
      <Drawer
        open={!!user}
        onClose={onClose}
        title={name}
        subtitle={user && <>{user.username}{user.email ? ` · ${user.email}` : ""} · joined {relativeTime(user.created_at)}</>}
        footer={
          user && (
            <>
              <Button variant="secondary" size="sm" onClick={() => navigate(`/admin/users/${user.id}/library`)}><Library /> Open library</Button>
              <Button variant="ghost" size="sm" className="ml-auto text-destructive hover:text-destructive" disabled={isSelf || busy} onClick={() => setConfirmDelete(true)}>
                <Trash2 /> Delete
              </Button>
            </>
          )
        }
      >
        {user && (
          <>
            <DrawerSection title="Groups" description="Groups decide what this person can do.">
              <div className="flex flex-wrap items-center gap-1.5">
                {(user.roles || []).map((r) => {
                  const g = groups.find((x) => x.name === r);
                  return (
                    <Chip
                      key={r}
                      tone={r === "Administrator" ? "accent" : "neutral"}
                      removeLabel={`Remove from ${r}`}
                      onRemove={g && !busy ? () => void run(() => api.del(`/api/v1/admin/roles/${g.id}/members`, { user_ids: [user.id] }), `Removed from ${r}`, `Could not remove from ${r}`) : undefined}
                    >
                      {r}
                    </Chip>
                  );
                })}
                {!(user.roles || []).length && <span className="text-sm text-muted">Not in any group, so they can not do anything yet.</span>}
              </div>
              {addable.length > 0 && (
                <div className="mt-3 max-w-xs">
                  <Select
                    value=""
                    placeholder="Add to a group"
                    onValueChange={(gid) => {
                      const g = groups.find((x) => x.id === gid);
                      if (g) void run(() => api.post(`/api/v1/admin/roles/${g.id}/members`, { user_ids: [user.id] }), `Added to ${g.name}`, `Could not add to ${g.name}`);
                    }}
                    options={addable.map((g) => ({ value: g.id, label: g.name }))}
                  />
                </div>
              )}
            </DrawerSection>

            <DrawerSection title="Upload limit" description={`Uploaded ${formatBytes(used)} of ${capLabel(effectiveCap)}.`}>
              {effectiveCap > 0 && <div className="mb-3"><Progress value={(used / effectiveCap) * 100} /></div>}
              <div className="flex flex-wrap items-center gap-2">
                <Select
                  className="w-48"
                  value={limitMode}
                  onValueChange={(v) => setLimitMode(v as "default" | "custom")}
                  options={[{ value: "default", label: `Default (${capLabel(defaultCap)})` }, { value: "custom", label: "Custom limit" }]}
                />
                {limitMode === "custom" && <ByteInput value={limit} onChange={setLimit} placeholder="0 = unlimited" />}
                {limitDirty && (
                  <Button size="sm" disabled={busy} onClick={() => void run(() => saveQuotas({ user: { id: user.id, bytes: nextOverride } }), "Upload limit saved", "Could not save limit")}>
                    Save
                  </Button>
                )}
              </div>
            </DrawerSection>

            <DrawerSection title="Account">
              <SettingSwitch
                label="Can sign in"
                description={isSelf ? "You cannot disable your own account." : user.disabled ? "Disabled. Sign-in and API keys are blocked." : "Turning this off signs them out everywhere."}
                checked={!user.disabled}
                disabled={isSelf || busy}
                onChange={(on) => void run(() => api.patch(`/api/v1/admin/users/${user.id}`, { disabled: !on }), on ? "Account enabled" : "Account disabled", "Could not change account")}
              />
            </DrawerSection>

            <DrawerSection title="Discord">
              {user.discord_id ? (
                <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border px-3 py-2">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium">{user.discord_username || "Linked account"}</div>
                    <div className="truncate text-xs text-subtle">{user.discord_id}</div>
                  </div>
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmUnlink(true)}><Unlink /> Unlink</Button>
                </div>
              ) : (
                <p className="text-sm text-muted">Not linked. They can link Discord from their own settings.</p>
              )}
            </DrawerSection>
          </>
        )}
      </Drawer>
      <ConfirmDialog
        open={confirmUnlink}
        onOpenChange={setConfirmUnlink}
        title="Unlink Discord?"
        description="They can no longer sign in with Discord until they link it again. Their account stays."
        confirmLabel="Unlink"
        onConfirm={() => { if (user) void run(() => api.del(`/api/v1/admin/users/${user.id}/identities/discord`), "Discord unlinked", "Could not unlink Discord"); }}
      />
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`Delete ${name}?`}
        description="This removes the account, sessions, playlists and Discord link. It cannot be undone."
        confirmLabel="Delete forever"
        destructive
        onConfirm={() => {
          if (!user) return;
          void run(() => api.del(`/api/v1/admin/users/${user.id}`), "User deleted", "Could not delete user").then((ok) => { if (ok) onClose(); });
        }}
      />
    </>
  );
}
