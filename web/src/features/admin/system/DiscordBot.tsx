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
import { Card, RowMenu, SettingSwitch, errorMessage } from "../adminUi";
import { Link } from "react-router-dom";

export const discordKey = ["discord"] as const;

/** Discord > Bot: token, gateway status and slash commands. */
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

      <Card title="Servers" description="Queues, libraries, bans and activity for each server are on the Servers page.">
        <Button size="sm" variant="secondary" asChild>
          <Link to="/admin/discord/servers">Manage servers</Link>
        </Button>
      </Card>
    </div>
  );
}
