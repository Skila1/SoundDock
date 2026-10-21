import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link2, MessageCircle, Plus, RefreshCcw, ShieldCheck, Trash2, UsersRound } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { errorMessage } from "./adminUi";
import {
  Avatar,
  Chip,
  DiscordRolePicker,
  DiscordServerSelect,
  Drawer,
  DrawerSection,
  groupsKey,
  useAdminUsers,
  useDiscordGuilds,
  useDiscordRoleName,
  useGroups,
  userLabel,
  usersKey,
  type AdminUserRow,
  type DiscordGuild,
  type DiscordLink,
  type GroupRow
} from "./iam/iamShared";
import { areasFor, permissionLabel } from "./iam/permissions";

type Permission = { name: string; description: string };

export function AdminRoles() {
  const qc = useQueryClient();
  const groups = useGroups();
  const users = useAdminUsers();
  const guilds = useDiscordGuilds();
  const perms = useQuery({ queryKey: ["admin-permissions"], queryFn: () => api.get<Permission[]>("/api/v1/admin/permissions") });
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected = (groups.data || []).find((g) => g.id === selectedId) || null;
  const refresh = async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: groupsKey }), qc.invalidateQueries({ queryKey: usersKey })]);
  };

  return (
    <div>
      <PageHeader
        title="Groups"
        description="Groups decide what people can do. Put someone in a group and they get its permissions."
        actions={<Button onClick={() => setCreateOpen(true)}><Plus /> New group</Button>}
      />
      {groups.isError ? (
        <QueryError message={errorMessage(groups.error, "Could not load groups")} onRetry={() => groups.refetch()} />
      ) : !groups.isLoading && !groups.data?.length ? (
        <EmptyState icon={UsersRound} title="No groups yet" action={{ label: "New group", onClick: () => setCreateOpen(true) }} />
      ) : (
        <ul className="divide-y divide-border overflow-hidden rounded-2xl border border-border bg-surface-1 shadow-sm">
          {(groups.data || []).map((g) => (
            <li key={g.id}>
              <button type="button" className="flex w-full items-center gap-4 px-4 py-4 text-left hover:bg-surface-2/60 focus-visible:bg-surface-2/60 focus-visible:outline-none sm:px-5" onClick={() => setSelectedId(g.id)}>
                <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-accent/10 text-accent">
                  {g.name === "Administrator" ? <ShieldCheck className="h-5 w-5" /> : <UsersRound className="h-5 w-5" />}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="font-semibold">{g.name}</span>
                    {g.is_system && <Badge>Built-in</Badge>}
                    {!!g.discord_links?.length && <Badge tone="accent"><MessageCircle className="h-3 w-3" /> Discord</Badge>}
                  </span>
                  <span className="mt-0.5 block truncate text-sm text-muted">{g.description || summary(g)}</span>
                </span>
                <span className="shrink-0 text-right">
                  <span className="tabular block text-lg font-semibold leading-tight">{g.member_count ?? 0}</span>
                  <span className="block text-xs text-subtle">{g.member_count === 1 ? "member" : "members"}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}

      <CreateGroupDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={async (id) => {
          await refresh();
          setSelectedId(id || null);
        }}
      />
      <GroupDrawer
        group={selected}
        permissions={perms.data || []}
        users={users.data || []}
        guilds={guilds.data || []}
        onClose={() => setSelectedId(null)}
        onChanged={refresh}
      />
    </div>
  );
}

function summary(g: GroupRow) {
  if (g.name === "Administrator") return "Everything, including these settings";
  const p = g.permissions || [];
  if (!p.length) return "No permissions";
  const shown = p.slice(0, 3).map(permissionLabel).join(", ");
  return p.length > 3 ? `${shown} and ${p.length - 3} more` : shown;
}

function CreateGroupDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (v: boolean) => void; onCreated: (id?: string) => void }) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="New group">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            try {
              const r = await api.post<{ id?: string }>("/api/v1/admin/roles", { name, description: desc, permissions: ["tracks.read", "tracks.stream"] });
              toast.success(`${name} created`);
              setName("");
              setDesc("");
              onOpenChange(false);
              onCreated(r?.id);
            } catch (err) {
              toast.error(errorMessage(err, "Could not create group"));
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Family" required /></Field>
          <Field label="Description" hint="Optional. Starts with browse and play; pick more after."><Input value={desc} onChange={(e) => setDesc(e.target.value)} /></Field>
          <div className="flex justify-end"><Button type="submit" disabled={busy}>Create group</Button></div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function GroupDrawer({
  group,
  permissions,
  users,
  guilds,
  onClose,
  onChanged
}: {
  group: GroupRow | null;
  permissions: Permission[];
  users: AdminUserRow[];
  guilds: DiscordGuild[];
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const groupId = group?.id;
  const permKey = (group?.permissions || []).join(",");
  useEffect(() => {
    if (!group) return;
    setName(group.name);
    setDesc(group.description || "");
    setPicked(new Set(group.permissions || []));
    // Reset only when a different group opens or the saved values change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupId, group?.name, group?.description, permKey]);

  const areas = useMemo(() => areasFor(permissions), [permissions]);
  const isAdmin = group?.name === "Administrator";
  const members = useMemo(() => (group ? users.filter((u) => (u.roles || []).includes(group.name)) : []), [users, group]);
  const others = useMemo(() => (group ? users.filter((u) => !(u.roles || []).includes(group.name)) : []), [users, group]);
  const dirty =
    !!group &&
    (name.trim() !== group.name ||
      desc.trim() !== (group.description || "") ||
      (!isAdmin && [...picked].sort().join(",") !== [...(group.permissions || [])].sort().join(",")));

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

  function save() {
    if (!group) return;
    void run(
      () =>
        api.patch(`/api/v1/admin/roles/${group.id}`, {
          name: group.is_system ? undefined : name.trim(),
          description: desc.trim(),
          permissions: isAdmin ? undefined : [...picked]
        }),
      "Group saved",
      "Could not save group"
    );
  }

  function toggle(perm: string, on: boolean) {
    const next = new Set(picked);
    if (on) next.add(perm);
    else next.delete(perm);
    setPicked(next);
  }

  return (
    <>
      <Drawer
        open={!!group}
        onClose={onClose}
        title={group?.name || "Group"}
        subtitle={group && `${members.length} ${members.length === 1 ? "member" : "members"}${group.is_system ? " · built-in" : ""}`}
        footer={
          group && (
            <>
              <Button size="sm" disabled={!dirty || busy} onClick={save}>{busy ? "Saving..." : "Save changes"}</Button>
              {dirty && <Button size="sm" variant="ghost" onClick={() => { setName(group.name); setDesc(group.description || ""); setPicked(new Set(group.permissions || [])); }}>Discard</Button>}
              {!group.is_system && (
                <Button size="sm" variant="ghost" className="ml-auto text-destructive hover:text-destructive" disabled={busy} onClick={() => setConfirmDelete(true)}>
                  <Trash2 /> Delete
                </Button>
              )}
            </>
          )
        }
      >
        {group && (
          <>
            <DrawerSection title="About">
              <div className="space-y-3">
                {!group.is_system && <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>}
                <Field label="Description"><Input value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="What this group is for" /></Field>
              </div>
            </DrawerSection>

            <DrawerSection title="Permissions">
              {isAdmin ? (
                <p className="rounded-xl bg-surface-2/60 px-3 py-2 text-sm text-muted">Administrators can do everything, including changing these settings.</p>
              ) : (
                <div className="space-y-4">
                  {areas.map((a) => (
                    <div key={a.id}>
                      <div className="mb-1 text-sm font-semibold">{a.label}</div>
                      <ul className="divide-y divide-border rounded-xl border border-border">
                        {a.perms.map((p) => (
                          <li key={p.name} className="flex items-center justify-between gap-3 px-3 py-2">
                            <span className="min-w-0">
                              <span className="block text-sm">{p.label}</span>
                              <span className="block text-xs text-subtle">{p.help}</span>
                            </span>
                            <Switch checked={picked.has(p.name)} aria-label={p.label} onCheckedChange={(v) => toggle(p.name, v)} />
                          </li>
                        ))}
                      </ul>
                    </div>
                  ))}
                </div>
              )}
            </DrawerSection>

            <DrawerSection title="Members">
              {others.length > 0 && (
                <div className="mb-3">
                  <Select
                    value=""
                    placeholder="Add a person"
                    onValueChange={(uid) => {
                      const u = users.find((x) => x.id === uid);
                      if (u) void run(() => api.post(`/api/v1/admin/roles/${group.id}/members`, { user_ids: [uid] }), `${userLabel(u)} added`, "Could not add member");
                    }}
                    options={others.map((u) => ({ value: u.id, label: userLabel(u) }))}
                  />
                </div>
              )}
              {members.length ? (
                <ul className="divide-y divide-border rounded-xl border border-border">
                  {members.map((u) => (
                    <li key={u.id} className="flex items-center gap-3 px-3 py-2">
                      <Avatar name={userLabel(u)} className="h-7 w-7 text-[10px]" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{userLabel(u)}</span>
                        <span className="block truncate text-xs text-subtle">{u.username}</span>
                      </span>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={busy}
                        onClick={() => void run(() => api.del(`/api/v1/admin/roles/${group.id}/members`, { user_ids: [u.id] }), `${userLabel(u)} removed`, "Could not remove member")}
                      >
                        Remove
                      </Button>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="text-sm text-muted">Nobody is in this group yet.</p>
              )}
            </DrawerSection>

            <DiscordLinks group={group} guilds={guilds} busy={busy} run={run} onChanged={onChanged} />
          </>
        )}
      </Drawer>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`Delete ${group?.name || "group"}?`}
        description="Members lose the permissions this group gave them. Their accounts stay."
        confirmLabel="Delete group"
        destructive
        onConfirm={() => {
          if (!group) return;
          void run(() => api.del(`/api/v1/admin/roles/${group.id}`), "Group deleted", "Could not delete group").then((ok) => { if (ok) onClose(); });
        }}
      />
    </>
  );
}

function DiscordLinks({ group, guilds, busy, run, onChanged }: { group: GroupRow; guilds: DiscordGuild[]; busy: boolean; run: (fn: () => Promise<unknown>, ok: string, fail: string) => Promise<boolean>; onChanged: () => Promise<void> }) {
  const links = group.discord_links || [];
  const [guildId, setGuildId] = useState("");
  const [roleId, setRoleId] = useState("");
  useEffect(() => {
    setGuildId(guilds[0]?.id || "");
    setRoleId("");
  }, [group.id, guilds]);
  const put = (next: DiscordLink[], ok: string) => run(() => api.put(`/api/v1/admin/roles/${group.id}/discord`, { links: next }), ok, "Could not update Discord link");

  return (
    <DrawerSection
      title="Discord role"
      description="People with this Discord role join the group when they sign in with Discord or when you sync."
      actions={
        links.length > 0 && (
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={async () => {
              try {
                const r = await api.post<{ synced: number; discord: boolean; error?: string }>("/api/v1/admin/roles/sync-discord");
                if (!r.discord) toast.message(r.error || "Discord unavailable, groups unchanged");
                else toast.success(r.synced ? `Added ${r.synced} ${r.synced === 1 ? "person" : "people"}` : "Already in sync");
                await onChanged();
              } catch (e) {
                toast.error(errorMessage(e, "Sync failed"));
              }
            }}
          >
            <RefreshCcw /> Sync now
          </Button>
        )
      }
    >
      {links.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-1.5">
          {links.map((l) => (
            <LinkChip
              key={`${l.guild_id}:${l.discord_role_id}`}
              link={l}
              guilds={guilds}
              onRemove={busy ? undefined : () => void put(links.filter((x) => x !== l), "Discord role unlinked")}
            />
          ))}
        </div>
      )}
      <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <DiscordServerSelect value={guildId} onChange={(v) => { setGuildId(v); setRoleId(""); }} guilds={guilds} />
        <DiscordRolePicker guildId={guildId} value={roleId} onChange={setRoleId} />
        <Button
          variant="secondary"
          disabled={busy || !guildId || !roleId}
          onClick={() => {
            if (links.some((l) => l.guild_id === guildId && l.discord_role_id === roleId)) return;
            void put([...links, { guild_id: guildId, discord_role_id: roleId }], "Discord role linked").then((ok) => { if (ok) setRoleId(""); });
          }}
        >
          <Link2 /> Link
        </Button>
      </div>
      {!guilds.length && <p className="mt-2 text-xs text-subtle">Invite the SoundDock bot to your server to pick it from a list.</p>}
    </DrawerSection>
  );
}

function LinkChip({ link, guilds, onRemove }: { link: DiscordLink; guilds: DiscordGuild[]; onRemove?: () => void }) {
  const roleName = useDiscordRoleName(link.guild_id, link.discord_role_id);
  const server = guilds.find((g) => g.id === link.guild_id)?.name || link.guild_id || "Any server";
  return (
    <Chip tone="accent" onRemove={onRemove} removeLabel="Unlink Discord role">
      {roleName ? `@${roleName}` : link.discord_role_id} in {server}
    </Chip>
  );
}
