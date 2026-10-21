import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCcw, Send, UserPlus } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { Card, RowMenu, SettingSwitch, errorMessage } from "../adminUi";
import { guildsKey, useDiscordGuilds } from "../iam/iamShared";

export const discordKey = ["discord"] as const;

/** Standalone page for System > Discord bot, if the bot moves out of IAM. */
export function AdminDiscordBot() {
  const d = useQuery({ queryKey: discordKey, queryFn: () => api.get<any>("/api/v1/admin/integrations/discord") });
  return (
    <div>
      <PageHeader title="Discord bot" description="The bot plays music in voice channels and answers slash commands. One bot covers every server it is invited to." />
      <DiscordBotSettings data={d.data} />
    </div>
  );
}

export function DiscordBotSettings({ data }: { data: any }) {
  const qc = useQueryClient();
  const st = useQuery({ queryKey: ["discord-status"], queryFn: () => api.get<any>("/api/v1/admin/integrations/discord/status") });
  const guilds = useDiscordGuilds();
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
  const gateway: string = data?.gateway_status || st.data?.gateway || "idle";
  const online = /ready|connected|online/i.test(gateway);

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
        description={
          <span className="inline-flex flex-wrap items-center gap-2">
            <Badge tone={online ? "success" : data?.token_configured ? "warning" : "neutral"}>{data?.token_configured ? `Gateway ${gateway}` : "No bot token"}</Badge>
            {st.data?.commands && <Badge>Commands {st.data.commands}</Badge>}
          </span>
        }
        actions={
          <>
            <Button size="sm" variant="secondary" onClick={() => api.get<any>("/api/v1/admin/integrations/discord/invite").then((r) => window.open(r.url, "_blank", "noopener")).catch((e) => toast.error(errorMessage(e, "Invite link unavailable")))}>
              <UserPlus /> Invite to a server
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
        {data?.last_error && <p className="mb-3 rounded-lg border border-destructive/40 px-3 py-2 text-sm text-destructive">{data.last_error}</p>}
        <SettingSwitch label="Bot enabled" checked={enabled} onChange={setEnabled} />
        <div className="mt-3 grid gap-3 md:grid-cols-2">
          <Field label="Application ID"><Input value={app} onChange={(e) => setApp(e.target.value)} /></Field>
          <Field label="Bot token" hint={data?.token_configured ? "Saved. Leave blank to keep it." : undefined}>
            <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder="••••••••" autoComplete="off" />
          </Field>
        </div>
        {dirty && (
          <div className="mt-4 flex gap-2">
            <Button size="sm" disabled={saving} onClick={save}>{saving ? "Saving..." : "Save bot settings"}</Button>
            <Button size="sm" variant="ghost" disabled={saving} onClick={reset}>Discard</Button>
          </div>
        )}
      </Card>

      <Card title="Servers" description="Turn a server off to ignore its slash commands and web playback.">
        {!guilds.data?.length && <p className="text-sm text-muted">{guilds.isLoading ? "Loading..." : "Invite the bot to a server to get started."}</p>}
        <ul className="divide-y divide-border">
          {(guilds.data || []).map((g) => {
            const sess: any = sessionsByGuild.get(g.id);
            return (
              <li key={g.id} className="flex flex-wrap items-center justify-between gap-3 py-2.5">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 font-medium">
                    {g.name || g.id}
                    {sess?.connected && <Badge tone="success">In voice</Badge>}
                  </div>
                  <div className="text-xs text-muted">
                    {sess?.status ? `${sess.status} · ` : ""}volume {g.default_volume ?? "-"} · queue limit {g.queue_limit ?? "-"}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={g.enabled !== false}
                    aria-label={`Enable ${g.name || g.id}`}
                    onCheckedChange={async (on) => {
                      try {
                        await api.patch(`/api/v1/admin/integrations/discord/guilds/${g.id}`, { enabled: on });
                        toast.success(on ? "Server enabled" : "Server disabled");
                        qc.invalidateQueries({ queryKey: guildsKey });
                      } catch (err) {
                        toast.error(errorMessage(err, "Could not update server"));
                      }
                    }}
                  />
                  <RowMenu
                    actions={[{
                      label: "Disconnect from voice",
                      onSelect: () => void api.post(`/api/v1/admin/integrations/discord/guilds/${g.id}/disconnect`).then(() => { toast("Disconnected"); qc.invalidateQueries({ queryKey: ["discord-sessions"] }); }).catch((e) => toast.error(errorMessage(e, "Could not disconnect")))
                    }]}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      </Card>
    </div>
  );
}
