import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { DiscordServerButton, HelpButton } from "@/components/community/CommunityLinks";
import { Card, SaveBar, SettingSwitch, errorMessage } from "./adminUi";
import { Chip, DiscordRolePicker, DiscordServerSelect, useAdminUsers, useDiscordGuilds, useDiscordRoleName, userLabel, type DiscordGuild } from "./iam/iamShared";
import { DiscordBotSettings, discordKey } from "./system/DiscordBot";

type RegistrationGuild = { guild_id: string; label: string; role_ids: string[] };
type GuildDraft = RegistrationGuild & { key: number };

let draftKey = 0;
const toDraft = (g?: RegistrationGuild): GuildDraft => ({ key: ++draftKey, guild_id: g?.guild_id || "", label: g?.label || "", role_ids: [...(g?.role_ids || [])] });
const clean = (list: RegistrationGuild[]) =>
  list.filter((g) => g.guild_id.trim()).map((g) => ({ guild_id: g.guild_id.trim(), label: g.label.trim(), role_ids: g.role_ids.filter(Boolean) }));

/**
 * Discord sign-in and who may join. The bot lives in its own section at the
 * bottom; pass bot={false} when the bot is shown under System instead.
 */
export function AdminDiscord({ bot = true }: { bot?: boolean }) {
  const d = useQuery({ queryKey: discordKey, queryFn: () => api.get<any>("/api/v1/admin/integrations/discord") });
  return (
    <div>
      <PageHeader
        title="Discord"
        description="Let people sign in with Discord, choose who may join, and pick which Discord accounts are administrators."
        actions={
          <>
            <HelpButton variant="secondary" />
            <DiscordServerButton />
          </>
        }
      />
      <SignInSettings data={d.data} />
      {bot && (
        <div className="mt-10 border-t border-border pt-8">
          <div className="mb-5">
            <h2 className="text-xl font-bold tracking-tight">Discord bot</h2>
            <p className="mt-1 text-sm text-muted">Separate from sign-in. The bot plays music in voice channels and answers slash commands.</p>
          </div>
          <DiscordBotSettings data={d.data} />
        </div>
      )}
    </div>
  );
}

function SignInSettings({ data }: { data: any }) {
  const qc = useQueryClient();
  const guilds = useDiscordGuilds();
  const users = useAdminUsers();
  const savedGuilds: RegistrationGuild[] = useMemo(() => data?.registration_guilds || [], [data]);
  const savedAdmins: string[] = useMemo(() => data?.admin_discord_ids || [], [data]);
  const [loginOn, setLoginOn] = useState(false);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [admins, setAdmins] = useState<string[]>([]);
  const [whitelistOn, setWhitelistOn] = useState(false);
  const [regGuilds, setRegGuilds] = useState<GuildDraft[]>([]);
  const [saving, setSaving] = useState(false);
  const reset = () => {
    setLoginOn(!!data?.login_enabled);
    setClientId(data?.client_id || "");
    setClientSecret("");
    setAdmins(savedAdmins);
    setWhitelistOn(!!data?.registration_whitelist_enabled);
    setRegGuilds(savedGuilds.map(toDraft));
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(reset, [data]);

  const dirty =
    loginOn !== !!data?.login_enabled ||
    clientId !== (data?.client_id || "") ||
    !!clientSecret ||
    admins.join(",") !== savedAdmins.join(",") ||
    whitelistOn !== !!data?.registration_whitelist_enabled ||
    JSON.stringify(clean(regGuilds)) !== JSON.stringify(clean(savedGuilds));

  async function save() {
    setSaving(true);
    try {
      await api.put("/api/v1/admin/integrations/discord", {
        login_enabled: loginOn,
        client_id: clientId || undefined,
        client_secret: clientSecret || undefined,
        // The OAuth client and the bot share one Discord application.
        application_id: !data?.application_id && clientId ? clientId : undefined,
        admin_discord_ids: admins,
        registration_whitelist_enabled: whitelistOn,
        registration_guilds: clean(regGuilds)
      });
      toast.success("Discord settings saved");
      setClientSecret("");
      qc.invalidateQueries({ queryKey: discordKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save settings"));
    } finally {
      setSaving(false);
    }
  }

  const linked = (users.data || []).filter((u) => u.discord_id);
  const nameFor = (id: string) => {
    const u = linked.find((x) => x.discord_id === id);
    return u ? `${userLabel(u)}${u.discord_username ? ` (@${u.discord_username})` : ""}` : id;
  };
  const adminOptions = linked.filter((u) => !admins.includes(u.discord_id || "")).map((u) => ({ value: u.discord_id as string, label: `${userLabel(u)}${u.discord_username ? ` (@${u.discord_username})` : ""}` }));
  const [adminDraft, setAdminDraft] = useState("");

  return (
    <div className="space-y-5">
      <Card
        title="Sign in with Discord"
        description="Username and password sign-in always keeps working."
        actions={<Badge tone={data?.login_ready ? "success" : "neutral"}>{data?.login_ready ? "Ready" : "Off"}</Badge>}
      >
        <SettingSwitch
          label="Allow Discord sign-in"
          description={!data?.secret_configured && !clientSecret ? "Add the client ID and secret below first." : undefined}
          checked={loginOn}
          onChange={setLoginOn}
        />
        <details className="group mt-2 rounded-xl border border-border px-4 py-3" open={!data?.secret_configured}>
          <summary className="cursor-pointer text-sm font-medium">Discord app credentials</summary>
          <div className="mt-3 grid gap-3 md:grid-cols-2">
            <Field label="Client ID"><Input value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="From the Discord developer portal" /></Field>
            <Field label="Client secret" hint={data?.secret_configured ? "Saved. Leave blank to keep it." : "Needed before sign-in can be turned on."}>
              <Input type="password" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} placeholder="••••••••" autoComplete="off" />
            </Field>
          </div>
          <p className="mt-3 text-xs text-subtle">Add this redirect URL to the OAuth2 settings of your Discord application:</p>
          <div className="mt-1 flex items-center gap-2">
            <code className="min-w-0 flex-1 break-all rounded-lg bg-surface-2 px-3 py-2 text-xs">{data?.oauth_redirect || "..."}</code>
            {data?.oauth_redirect && (
              <Button size="icon" variant="ghost" aria-label="Copy redirect URL" onClick={() => { void navigator.clipboard?.writeText(data.oauth_redirect); toast.success("Copied"); }}>
                <Copy />
              </Button>
            )}
          </div>
        </details>
      </Card>

      <Card title="Who can join" description="Applies to new Discord accounts. People already signed up are not affected.">
        <SettingSwitch
          label="Only members of chosen servers"
          description={whitelistOn ? "Someone may join if they are in any server below and, when roles are listed, hold one of them." : "Off: any Discord account can join."}
          checked={whitelistOn}
          onChange={setWhitelistOn}
        />
        {whitelistOn && (
          <>
            <ul className="mt-3 space-y-3">
              {regGuilds.map((g) => (
                <GuildRule
                  key={g.key}
                  draft={g}
                  guilds={guilds.data || []}
                  onChange={(patch) => setRegGuilds((cur) => cur.map((x) => (x.key === g.key ? { ...x, ...patch } : x)))}
                  onRemove={() => setRegGuilds((cur) => cur.filter((x) => x.key !== g.key))}
                />
              ))}
            </ul>
            {!regGuilds.length && <p className="mt-3 rounded-xl bg-warning/10 px-3 py-2 text-sm text-warning">Add at least one server, or turn this off, before saving.</p>}
            <Button
              type="button"
              variant="secondary"
              size="sm"
              className="mt-3"
              onClick={() => {
                const unused = (guilds.data || []).find((k) => !regGuilds.some((x) => x.guild_id === k.id));
                setRegGuilds((cur) => [...cur, toDraft(unused ? { guild_id: unused.id, label: unused.name || "", role_ids: [] } : undefined)]);
              }}
            >
              <Plus /> Add server
            </Button>
          </>
        )}
      </Card>

      <Card title="Administrators" description="These Discord accounts become SoundDock administrators when they sign in, and can always join.">
        <div className="flex flex-wrap gap-1.5">
          {admins.map((id) => (
            <Chip key={id} tone="accent" removeLabel="Remove administrator" onRemove={() => setAdmins((cur) => cur.filter((x) => x !== id))}>{nameFor(id)}</Chip>
          ))}
          {!admins.length && <span className="text-sm text-muted">Nobody yet.</span>}
        </div>
        <div className="mt-3 flex flex-col gap-2 sm:flex-row">
          {adminOptions.length > 0 && (
            <div className="sm:w-64">
              <Select value="" placeholder="Add a linked account" options={adminOptions} onValueChange={(id) => setAdmins((cur) => (cur.includes(id) ? cur : [...cur, id]))} />
            </div>
          )}
          <form
            className="flex flex-1 gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              const id = adminDraft.trim();
              if (!/^\d{15,22}$/.test(id)) {
                toast.error("A Discord user ID is 17 to 20 digits");
                return;
              }
              setAdmins((cur) => (cur.includes(id) ? cur : [...cur, id]));
              setAdminDraft("");
            }}
          >
            <Input className="sm:max-w-56" value={adminDraft} onChange={(e) => setAdminDraft(e.target.value)} inputMode="numeric" placeholder="Or paste a Discord user ID" aria-label="Discord user ID" />
            <Button type="submit" variant="secondary" disabled={!adminDraft.trim()}>Add</Button>
          </form>
        </div>
      </Card>
      <SaveBar dirty={dirty} saving={saving} onSave={save} onReset={reset} />
    </div>
  );
}

function GuildRule({ draft, guilds, onChange, onRemove }: { draft: GuildDraft; guilds: DiscordGuild[]; onChange: (patch: Partial<GuildDraft>) => void; onRemove: () => void }) {
  const [role, setRole] = useState("");
  return (
    <li className="rounded-xl border border-border p-3">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <DiscordServerSelect
            value={draft.guild_id}
            guilds={guilds}
            onChange={(id) => {
              const match = guilds.find((k) => k.id === id);
              onChange({ guild_id: id, label: match?.name || draft.label, role_ids: id === draft.guild_id ? draft.role_ids : [] });
            }}
          />
        </div>
        <Button type="button" size="icon" variant="ghost" aria-label="Remove server" onClick={onRemove}><Trash2 /></Button>
      </div>
      <div className="mt-3">
        <div className="mb-1.5 text-xs text-muted">{draft.role_ids.length ? "Must hold one of these roles:" : "Every member of this server may join. Add a role to narrow it down."}</div>
        <div className="flex flex-wrap gap-1.5">
          {draft.role_ids.map((rid) => (
            <RoleChip key={rid} guildId={draft.guild_id} roleId={rid} onRemove={() => onChange({ role_ids: draft.role_ids.filter((x) => x !== rid) })} />
          ))}
        </div>
        {draft.guild_id && (
          <div className="mt-2 flex gap-2">
            <div className="min-w-0 flex-1 sm:max-w-xs">
              <DiscordRolePicker guildId={draft.guild_id} value={role} onChange={setRole} placeholder="Pick a role" />
            </div>
            <Button
              type="button"
              variant="secondary"
              disabled={!role}
              onClick={() => {
                if (!draft.role_ids.includes(role)) onChange({ role_ids: [...draft.role_ids, role] });
                setRole("");
              }}
            >
              Add role
            </Button>
          </div>
        )}
      </div>
    </li>
  );
}

function RoleChip({ guildId, roleId, onRemove }: { guildId: string; roleId: string; onRemove: () => void }) {
  const name = useDiscordRoleName(guildId, roleId);
  return <Chip onRemove={onRemove} removeLabel="Remove role">{name ? `@${name}` : roleId}</Chip>;
}
