import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { cn } from "@/lib/utils";

export type AdminUserRow = {
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

export type DiscordLink = { guild_id: string; discord_role_id: string };

export type GroupRow = {
  id: string;
  name: string;
  description?: string;
  is_system?: boolean;
  member_count?: number;
  permissions?: string[];
  discord_links?: DiscordLink[];
};

export type DiscordGuild = { id: string; name?: string; enabled?: boolean; default_volume?: number; queue_limit?: number };
export type DiscordRole = { id: string; name: string; color?: number };

export const usersKey = ["admin-users"] as const;
export const groupsKey = ["admin-roles"] as const;
export const guildsKey = ["discord-guilds"] as const;

export function useAdminUsers() {
  return useQuery({ queryKey: usersKey, queryFn: () => api.get<AdminUserRow[]>("/api/v1/admin/users") });
}

export function useGroups() {
  return useQuery({ queryKey: groupsKey, queryFn: () => api.get<GroupRow[]>("/api/v1/admin/roles") });
}

/** Servers the bot has seen. Empty when the bot was never set up. */
export function useDiscordGuilds() {
  return useQuery({ queryKey: guildsKey, queryFn: () => api.get<DiscordGuild[]>("/api/v1/admin/integrations/discord/guilds").catch(() => [] as DiscordGuild[]) });
}

export function useDiscordRoles(guildId: string) {
  return useQuery({
    queryKey: ["discord-guild-roles", guildId],
    queryFn: () => api.get<{ roles: DiscordRole[]; discord: boolean; error?: string }>(`/api/v1/admin/integrations/discord/guilds/${encodeURIComponent(guildId)}/roles`),
    enabled: !!guildId,
    staleTime: 60_000,
    retry: false
  });
}

export function userLabel(u: { display_name?: string; username: string }) {
  return u.display_name || u.username;
}

export function initials(name: string) {
  const parts = name.trim().split(/[\s._-]+/).filter(Boolean);
  return ((parts[0]?.[0] || "?") + (parts[1]?.[0] || "")).toUpperCase();
}

export function Avatar({ name, className }: { name: string; className?: string }) {
  return (
    <span className={cn("flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent/15 text-xs font-bold text-accent", className)} aria-hidden>
      {initials(name)}
    </span>
  );
}

/** A removable pill, used for groups and Discord IDs. */
export function Chip({ children, onRemove, removeLabel, tone = "neutral" }: { children: ReactNode; onRemove?: () => void; removeLabel?: string; tone?: "neutral" | "accent" }) {
  return (
    <span className={cn("inline-flex max-w-full items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium", tone === "accent" ? "bg-accent/15 text-accent" : "bg-surface-2 text-foreground ring-1 ring-inset ring-border")}>
      <span className="truncate">{children}</span>
      {onRemove && (
        <button type="button" className="-mr-1 rounded-full p-0.5 text-muted hover:bg-surface-3 hover:text-foreground" aria-label={removeLabel || "Remove"} onClick={onRemove}>
          <X className="h-3 w-3" />
        </button>
      )}
    </span>
  );
}

/** Right-hand panel for editing one row. Full width on phones. */
export function Drawer({ open, onClose, title, subtitle, children, footer }: { open: boolean; onClose: () => void; title: string; subtitle?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  return (
    <Sheet open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
      <SheetContent title={title} className="flex w-[min(480px,100vw)] flex-col p-0 [&>h2]:px-5 [&>h2]:pt-5 [&>h2]:pr-12 [&>h2]:text-lg [&>h2]:mb-0">
        {subtitle && <div className="px-5 pt-1 text-sm text-muted">{subtitle}</div>}
        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-5 py-5 scrollbar-thin">{children}</div>
        {footer && <div className="flex flex-wrap items-center gap-2 border-t border-border px-5 py-3">{footer}</div>}
      </SheetContent>
    </Sheet>
  );
}

export function DrawerSection({ title, description, actions, children }: { title: string; description?: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <section>
      <div className="mb-2 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-subtle">{title}</h3>
          {description && <p className="mt-0.5 text-xs text-muted">{description}</p>}
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}

/** Server dropdown fed by the bot's server list, with a raw ID box when the bot knows none. */
export function DiscordServerSelect({ value, onChange, guilds, allowNone, noneLabel = "Not linked" }: { value: string; onChange: (id: string) => void; guilds: DiscordGuild[]; allowNone?: boolean; noneLabel?: string }) {
  const known = guilds.some((g) => g.id === value);
  if (!guilds.length || (value && !known)) {
    return <Input value={value} inputMode="numeric" placeholder="Server ID" aria-label="Discord server ID" onChange={(e) => onChange(e.target.value.trim())} />;
  }
  const options = [
    ...(allowNone ? [{ value: "__none", label: noneLabel }] : []),
    ...guilds.map((g) => ({ value: g.id, label: g.name || g.id }))
  ];
  return <Select value={value || (allowNone ? "__none" : "")} onValueChange={(v) => onChange(v === "__none" ? "" : v)} options={options} placeholder="Pick a server" />;
}

/**
 * Role dropdown for one server. When Discord cannot list the roles (bot off,
 * no permission, offline) it falls back to a role ID box.
 */
export function DiscordRolePicker({ guildId, value, onChange, placeholder = "Pick a role" }: { guildId: string; value: string; onChange: (id: string) => void; placeholder?: string }) {
  const roles = useDiscordRoles(guildId);
  const list = roles.data?.roles || [];
  const usable = !!guildId && roles.data?.discord && list.length > 0 && (!value || list.some((r) => r.id === value));
  if (!usable) {
    return (
      <div className="space-y-1">
        <Input value={value} inputMode="numeric" placeholder="Role ID" aria-label="Discord role ID" disabled={!guildId} onChange={(e) => onChange(e.target.value.trim())} />
        {guildId && !roles.isLoading && roles.data && !roles.data.discord && <p className="text-xs text-subtle">{roles.data.error || "Discord did not return roles"}, so type the role ID.</p>}
      </div>
    );
  }
  return <Select value={value} onValueChange={onChange} options={list.map((r) => ({ value: r.id, label: r.name }))} placeholder={roles.isLoading ? "Loading roles" : placeholder} />;
}

/** Name for a Discord role ID when the server's roles are known. */
export function useDiscordRoleName(guildId: string, roleId: string) {
  const roles = useDiscordRoles(guildId);
  return roles.data?.roles?.find((r) => r.id === roleId)?.name;
}
