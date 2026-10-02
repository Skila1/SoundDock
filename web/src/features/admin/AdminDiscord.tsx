import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCcw, Send, Trash2, UserPlus } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { DiscordServerButton, HelpButton } from "@/components/community/CommunityLinks";
import { AdminTabs, Card, RowMenu, SaveBar, SettingSwitch, errorMessage } from "./adminUi";

type RegistrationGuild = { guild_id: string; label: string; role_ids: string[] };
type GuildDraft = { key: number; guild_id: string; label: string; roles: string };

let draftKey = 0;
const toDraft = (g?: RegistrationGuild): GuildDraft => ({ key: ++draftKey, guild_id: g?.guild_id || "", label: g?.label || "", roles: (g?.role_ids || []).join(", ") });
const fromDrafts = (list: GuildDraft[]) =>
  list
    .filter((g) => g.guild_id.trim() || g.roles.trim())
    .map((g) => ({ guild_id: g.guild_id.trim(), label: g.label.trim(), role_ids: g.roles.split(/[\s,]+/).map((r) => r.trim()).filter(Boolean) }));

const discordKey = ["discord"];

export function AdminDiscord() {
  const d = useQuery({ queryKey: discordKey, queryFn: () => api.get<any>("/api/v1/admin/integrations/discord") });
  const st = useQuery({ queryKey: ["discord-status"], queryFn: () => api.get<any>("/api/v1/admin/integrations/discord/status") });
  return (
    <div>
      <PageHeader
        title="Discord"
        description="Optional. Let people sign in with Discord, choose who may join, and run the SoundDock bot."
        actions={
          <div className="flex flex-wrap gap-2">
            <HelpButton variant="secondary" />
            <DiscordServerButton />
          </div>
        }
      />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.data?.login_ready ? "success" : "neutral"}>{d.data?.login_ready ? "Sign-in on" : "Sign-in off"}</Badge>
        <Badge tone={d.data?.token_configured ? "success" : "neutral"}>{d.data?.token_configured ? "Bot configured" : "No bot token"}</Badge>
        <Badge>Gateway: {d.data?.gateway_status || st.data?.gateway || "idle"}</Badge>
        {st.data?.commands && <Badge>Commands: {st.data.commands}</Badge>}
      </div>
      {d.data?.last_error && <p className="mb-4 rounded-lg border border-destructive/40 px-3 py-2 text-sm text-destructive">{d.data.last_error}</p>}
      <AdminTabs
        fallback="access"
        tabs={[
          { id: "access", label: "Sign-in & access", content: <AccessTab data={d.data} /> },
          { id: "bot", label: "Bot & servers", content: <BotTab data={d.data} /> }
        ]}
      />
    </div>
  );
}

function AccessTab({ data }: { data: any }) {
  const qc = useQueryClient();
  const guilds = useQuery({ queryKey: ["discord-guilds"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/discord/guilds") });
  const initial = useMemo(
    () => ({
      loginOn: !!data?.login_enabled,
      clientId: data?.client_id || "",
      adminIds: (data?.admin_discord_ids || []).join(", "),
      whitelistOn: !!data?.registration_whitelist_enabled,
      guilds: JSON.stringify(data?.registration_guilds || [])
    }),
    [data]
  );
  const [loginOn, setLoginOn] = useState(false);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [adminIds, setAdminIds] = useState("");
  const [whitelistOn, setWhitelistOn] = useState(false);
  const [regGuilds, setRegGuilds] = useState<GuildDraft[]>([]);
  const [saving, setSaving] = useState(false);
  const reset = () => {
    setLoginOn(initial.loginOn);
    setClientId(initial.clientId);
    setClientSecret("");
    setAdminIds(initial.adminIds);
    setWhitelistOn(initial.whitelistOn);
    setRegGuilds(((data?.registration_guilds || []) as RegistrationGuild[]).map(toDraft));
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(reset, [initial]);

  const dirty =
    loginOn !== initial.loginOn ||
    clientId !== initial.clientId ||
    !!clientSecret ||
    adminIds !== initial.adminIds ||
    whitelistOn !== initial.whitelistOn ||
    JSON.stringify(fromDrafts(regGuilds)) !== JSON.stringify(fromDrafts(((data?.registration_guilds || []) as RegistrationGuild[]).map(toDraft)));

  async function save() {
    setSaving(true);
    try {
      await api.put("/api/v1/admin/integrations/discord", {
        login_enabled: loginOn,
        client_id: clientId || undefined,
        client_secret: clientSecret || undefined,
        // The OAuth client and the bot share one Discord application.
        application_id: !data?.application_id && clientId ? clientId : undefined,
        admin_discord_ids: adminIds.split(",").map((s) => s.trim()).filter(Boolean),
        registration_whitelist_enabled: whitelistOn,
        registration_guilds: fromDrafts(regGuilds)
      });
      toast.success("Discord sign-in settings saved");
      setClientSecret("");
      qc.invalidateQueries({ queryKey: discordKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save settings"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-5">
      <Card title="Sign in with Discord" description="Username and password sign-in always keeps working.">
        <SettingSwitch label="Allow Discord sign-in" checked={loginOn} onChange={setLoginOn} />
        <div className="mt-3 grid gap-3 md:grid-cols-2">
          <Field label="Client ID"><Input value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="From the Discord developer portal" /></Field>
          <Field label="Client secret" hint={data?.secret_configured ? "Saved. Leave blank to keep it." : "Needed before sign-in can be turned on."}>
            <Input type="password" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} placeholder="••••••••" />
          </Field>
        </div>
        <p className="mt-3 text-xs text-subtle">Add this redirect URL to the OAuth2 settings of your Discord application:</p>
        <code className="mt-1 block break-all rounded-lg bg-surface-2 px-3 py-2 text-xs">{data?.oauth_redirect || "…"}</code>
      </Card>

      <Card title="Administrators" description="These Discord accounts become SoundDock administrators when they sign in, and can always join.">
        <Field label="Discord user IDs" hint="Numeric IDs, separated by commas.">
          <Input value={adminIds} onChange={(e) => setAdminIds(e.target.value)} placeholder="123456789012345678" />
        </Field>
      </Card>

      <Card title="Who can join" description="Applies to new Discord accounts only. Someone may join if they are in any listed server and, when roles are listed, hold one of them.">
        <SettingSwitch label="Only allow members of these servers" description="Off lets any Discord account join." checked={whitelistOn} onChange={setWhitelistOn} />
        <datalist id="discord-known-guilds">
          {(guilds.data || []).map((g) => <option key={g.id} value={g.id}>{g.name || g.id}</option>)}
        </datalist>
        <ul className="mt-3 space-y-3">
          {regGuilds.map((g) => {
            const set = (patch: Partial<GuildDraft>) => setRegGuilds((cur) => cur.map((x) => (x.key === g.key ? { ...x, ...patch } : x)));
            const known = (guilds.data || []).find((k) => k.id === g.guild_id.trim());
            return (
              <li key={g.key} className="rounded-lg border border-border p-3">
                <div className="grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
                  <Field label="Server ID" hint={known ? `Bot sees: ${known.name || known.id}` : undefined}>
                    <Input
                      value={g.guild_id}
                      list="discord-known-guilds"
                      inputMode="numeric"
                      onChange={(e) => {
                        const id = e.target.value;
                        const match = (guilds.data || []).find((k) => k.id === id.trim());
                        set({ guild_id: id, ...(match && !g.label ? { label: match.name || "" } : {}) });
                      }}
                    />
                  </Field>
                  <Field label="Label (optional)"><Input value={g.label} onChange={(e) => set({ label: e.target.value })} placeholder="Main community" /></Field>
                  <div className="flex items-end">
                    <Button type="button" size="icon" variant="ghost" aria-label="Remove server" onClick={() => setRegGuilds((cur) => cur.filter((x) => x.key !== g.key))}><Trash2 /></Button>
                  </div>
                </div>
                <div className="mt-3">
                  <Field label="Required role IDs (optional)" hint="Any one of these roles is enough. Leave blank to allow every member.">
                    <Input value={g.roles} onChange={(e) => set({ roles: e.target.value })} />
                  </Field>
                </div>
              </li>
            );
          })}
        </ul>
        <Button type="button" variant="secondary" size="sm" className="mt-3" onClick={() => setRegGuilds((cur) => [...cur, toDraft()])}><Plus /> Add server</Button>
      </Card>
      <SaveBar dirty={dirty} saving={saving} onSave={save} onReset={reset} />
    </div>
  );
}

function BotTab({ data }: { data: any }) {
  const qc = useQueryClient();
  const guilds = useQuery({ queryKey: ["discord-guilds"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/discord/guilds") });
  const sessions = useQuery({ queryKey: ["discord-sessions"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/discord/sessions"), refetchInterval: 10000 });
  const [enabled, setEnabled] = useState(false);
  const [app, setApp] = useState("");
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const reset = () => {
    setEnabled(!!data?.enabled);
    setApp(data?.application_id || "");
    setToken("");
  };
  useEffect(reset, [data]);
  const dirty = enabled !== !!data?.enabled || app !== (data?.application_id || "") || !!token;

  async function save() {
    setSaving(true);
    try {
      await api.put("/api/v1/admin/integrations/discord", { enabled, token: token || undefined, application_id: app || undefined });
      toast.success("Bot settings saved");
      setToken("");
      qc.invalidateQueries({ queryKey: discordKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save bot settings"));
    } finally {
      setSaving(false);
    }
  }

  const sessionsByGuild = new Map((sessions.data || []).map((s: any) => [s.guild_id, s]));

  return (
    <div className="space-y-5">
      <Card
        title="Bot"
        description="One bot covers every server it is invited to."
        actions={
          <>
            <Button size="sm" variant="secondary" onClick={() => api.get<any>("/api/v1/admin/integrations/discord/invite").then((r) => window.open(r.url, "_blank")).catch((e) => toast.error(errorMessage(e, "Invite link unavailable")))}>
              <UserPlus /> Invite bot
            </Button>
            <RowMenu
              actions={[
                { label: "Test connection", icon: Send, onSelect: () => void api.post<any>("/api/v1/admin/integrations/discord/test").then((r) => toast(r?.ok ? "Connection looks good" : "Bot is not ready")).catch((e) => toast.error(errorMessage(e, "Test failed"))) },
                { label: "Re-register slash commands", icon: RefreshCcw, onSelect: () => void api.post("/api/v1/admin/integrations/discord/commands/sync").then(() => toast.success("Command sync requested")).catch((e) => toast.error(errorMessage(e, "Sync failed"))) }
              ]}
            />
          </>
        }
      >
        <SettingSwitch label="Bot enabled" checked={enabled} onChange={setEnabled} />
        <div className="mt-3 grid gap-3 md:grid-cols-2">
          <Field label="Application ID"><Input value={app} onChange={(e) => setApp(e.target.value)} /></Field>
          <Field label="Bot token" hint={data?.token_configured ? "Saved. Leave blank to keep it." : undefined}><Input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder="••••••••" /></Field>
        </div>
      </Card>

      <Card title="Servers" description="Turn a server off to ignore its slash commands and web playback.">
        {!guilds.data?.length && <p className="text-sm text-muted">{guilds.isLoading ? "Loading…" : "Invite the bot to a server to get started."}</p>}
        <ul className="divide-y divide-border">
          {(guilds.data || []).map((g) => {
            const sess: any = sessionsByGuild.get(g.id);
            return (
              <li key={g.id} className="flex flex-wrap items-center justify-between gap-3 py-2">
                <div className="min-w-0">
                  <div className="font-medium">{g.name || g.id}</div>
                  <div className="text-xs text-muted">
                    {sess?.connected ? "In voice" : "Not in voice"}{sess?.status ? ` · ${sess.status}` : ""} · volume {g.default_volume} · queue limit {g.queue_limit}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={g.enabled !== false}
                    onCheckedChange={async (on) => {
                      try {
                        await api.patch(`/api/v1/admin/integrations/discord/guilds/${g.id}`, { enabled: on });
                        toast.success(on ? "Server enabled" : "Server disabled");
                        qc.invalidateQueries({ queryKey: ["discord-guilds"] });
                      } catch (err) {
                        toast.error(errorMessage(err, "Could not update server"));
                      }
                    }}
                  />
                  <RowMenu
                    actions={[{
                      label: "Disconnect from voice",
                      onSelect: () => void api.post(`/api/v1/admin/integrations/discord/guilds/${g.id}/disconnect`).then(() => { toast("Disconnected"); qc.invalidateQueries({ queryKey: ["discord-sessions"] }); })
                    }]}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      </Card>
      <SaveBar dirty={dirty} saving={saving} onSave={save} onReset={reset} />
    </div>
  );
}
