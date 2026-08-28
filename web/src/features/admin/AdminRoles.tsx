import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { PageHeader } from "@/components/ui/empty";
import { Badge } from "@/components/ui/misc";
import { toast } from "sonner";

type Permission = { name: string; description: string };
type DiscordLink = { guild_id: string; discord_role_id: string };
type RoleRow = {
  id: string;
  name: string;
  description?: string;
  is_system?: boolean;
  member_count?: number;
  permissions?: string[];
  discord_links?: DiscordLink[];
};
type AdminUser = { id: string; username: string; display_name?: string; discord_id?: string | null };

export function AdminRoles() {
  const qc = useQueryClient();
  const roles = useQuery({ queryKey: ["admin-roles"], queryFn: () => api.get<RoleRow[]>("/api/v1/admin/roles") });
  const perms = useQuery({ queryKey: ["admin-permissions"], queryFn: () => api.get<Permission[]>("/api/v1/admin/permissions") });
  const users = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<AdminUser[]>("/api/v1/admin/users") });
  const guilds = useQuery({ queryKey: ["discord-guilds"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/discord/guilds").catch(() => []) });
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [selected, setSelected] = useState<RoleRow | null>(null);

  return (
    <div>
      <PageHeader
        title="Groups"
        description="SoundDock permissions are the source of truth. Discord role links only add people to a group when they sign in with Discord, and are optional."
        actions={<Button onClick={() => setCreateOpen(true)}>Create group</Button>}
      />
      <div className="grid gap-3 md:grid-cols-2">
        {(roles.data || []).map((r) => (
          <button
            key={r.id}
            type="button"
            className="rounded-xl border border-border bg-surface-1 p-4 text-left hover:border-accent/40"
            onClick={() => setSelected(r)}
          >
            <div className="flex items-center justify-between gap-2">
              <h3 className="font-semibold">{r.name}</h3>
              {r.is_system ? <Badge>Built-in</Badge> : <Badge tone="success">Custom</Badge>}
            </div>
            <p className="mt-1 text-sm text-muted">{r.description || "No description"}</p>
            <p className="mt-3 text-xs text-subtle">{r.member_count ?? 0} members · {(r.permissions || []).length} permissions</p>
          </button>
        ))}
      </div>
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent title="Create group">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await api.post("/api/v1/admin/roles", { name, description: desc, permissions: ["tracks.read", "tracks.stream"] });
              toast.success("Group created");
              setCreateOpen(false);
              setName("");
              setDesc("");
              qc.invalidateQueries({ queryKey: ["admin-roles"] });
            }}
          >
            <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} required /></Field>
            <Field label="Description"><Input value={desc} onChange={(e) => setDesc(e.target.value)} /></Field>
            <Button type="submit">Create</Button>
          </form>
        </DialogContent>
      </Dialog>
      <EditRoleDialog
        role={selected}
        permissions={perms.data || []}
        users={users.data || []}
        guilds={guilds.data || []}
        onClose={() => setSelected(null)}
        onChanged={() => qc.invalidateQueries({ queryKey: ["admin-roles"] })}
      />
    </div>
  );
}

function EditRoleDialog({
  role,
  permissions,
  users,
  guilds,
  onClose,
  onChanged
}: {
  role: RoleRow | null;
  permissions: Permission[];
  users: AdminUser[];
  guilds: { id: string; name?: string }[];
  onClose: () => void;
  onChanged: () => void;
}) {
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [desc, setDesc] = useState("");
  const [memberIds, setMemberIds] = useState<Set<string>>(new Set());
  const [guildId, setGuildId] = useState("");
  const [discordRole, setDiscordRole] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!role) return;
    setPicked(new Set(role.permissions || []));
    setDesc(role.description || "");
    setMemberIds(new Set());
    const link = (role.discord_links || [])[0];
    setGuildId(link?.guild_id || (guilds[0]?.id || ""));
    setDiscordRole(link?.discord_role_id || "");
  }, [role, guilds]);
  const catalog = useMemo(
    () => permissions.filter((p) => p.name !== "admin"),
    [permissions]
  );
  async function save() {
    if (!role) return;
    setBusy(true);
    try {
      await api.patch(`/api/v1/admin/roles/${role.id}`, {
        description: desc,
        permissions: role.name === "Administrator" ? undefined : [...picked]
      });
      if (memberIds.size) {
        await api.post(`/api/v1/admin/roles/${role.id}/members`, { user_ids: [...memberIds] });
      }
      await api.put(`/api/v1/admin/roles/${role.id}/discord`, {
        links: discordRole.trim() ? [{ guild_id: guildId, discord_role_id: discordRole.trim() }] : []
      });
      toast.success("Group saved");
      onChanged();
      onClose();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save group");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open={!!role} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title={role ? role.name : "Group"}>
        {role && (
          <div className="space-y-4">
            <Field label="Description">
              <Input value={desc} onChange={(e) => setDesc(e.target.value)} />
            </Field>
            <div>
              <div className="mb-2 text-sm font-medium">Permissions</div>
              {role.name === "Administrator" ? (
                <p className="text-sm text-muted">Administrator always has every permission.</p>
              ) : (
                <div className="max-h-48 space-y-1 overflow-y-auto rounded-lg border border-border p-2 scrollbar-thin">
                  {catalog.map((p) => (
                    <label key={p.name} className="flex items-start gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-surface-2">
                      <input
                        type="checkbox"
                        className="mt-1"
                        checked={picked.has(p.name)}
                        onChange={() => {
                          const next = new Set(picked);
                          if (next.has(p.name)) next.delete(p.name);
                          else next.add(p.name);
                          setPicked(next);
                        }}
                      />
                      <span>
                        <span className="font-medium">{p.name}</span>
                        <span className="block text-xs text-subtle">{p.description}</span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
            <div>
              <div className="mb-2 text-sm font-medium">Add members</div>
              <div className="max-h-36 space-y-1 overflow-y-auto rounded-lg border border-border p-2 scrollbar-thin">
                {users.map((u) => (
                  <label key={u.id} className="flex items-center gap-2 px-2 py-1 text-sm">
                    <input
                      type="checkbox"
                      checked={memberIds.has(u.id)}
                      onChange={() => {
                        const next = new Set(memberIds);
                        if (next.has(u.id)) next.delete(u.id);
                        else next.add(u.id);
                        setMemberIds(next);
                      }}
                    />
                    {u.display_name || u.username}
                  </label>
                ))}
              </div>
            </div>
            <div className="rounded-lg border border-border p-3">
              <div className="text-sm font-medium">Discord role (optional)</div>
              <p className="mb-2 text-xs text-subtle">Maps a Discord role onto this group. SoundDock still decides what the group can do. If Discord is down, nothing here changes.</p>
              <div className="grid gap-2 sm:grid-cols-2">
                <Field label="Server">
                  <select className="h-10 w-full rounded-lg border border-border bg-surface-2 px-3 text-sm" value={guildId} onChange={(e) => setGuildId(e.target.value)}>
                    <option value="">Not linked</option>
                    {guilds.map((g) => (
                      <option key={g.id} value={g.id}>{g.name || g.id}</option>
                    ))}
                  </select>
                </Field>
                <Field label="Discord role ID">
                  <Input value={discordRole} onChange={(e) => setDiscordRole(e.target.value)} placeholder="Optional snowflake" />
                </Field>
              </div>
              <Button
                className="mt-2"
                type="button"
                variant="secondary"
                size="sm"
                onClick={async () => {
                  const r = await api.post<{ synced: number; discord: boolean; error?: string }>("/api/v1/admin/roles/sync-discord");
                  if (!r.discord) toast.message(r.error || "Discord unavailable; groups unchanged");
                  else toast.success(`Linked ${r.synced} memberships`);
                  onChanged();
                }}
              >
                Sync Discord memberships
              </Button>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button type="button" disabled={busy} onClick={save}>Save group</Button>
              {!role.is_system && (
                <Button
                  type="button"
                  variant="destructive"
                  disabled={busy}
                  onClick={async () => {
                    await api.del(`/api/v1/admin/roles/${role.id}`);
                    toast.success("Group deleted");
                    onChanged();
                    onClose();
                  }}
                >
                  Delete
                </Button>
              )}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
