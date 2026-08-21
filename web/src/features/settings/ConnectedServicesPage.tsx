import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Link2, Radio } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import { ensureDiscordPresence, setDiscordPresenceEnabled } from "@/features/settings/discordPresence";

const labels: Record<string, string> = {
  spotify: "Spotify",
  youtube: "YouTube",
  soundcloud: "SoundCloud",
  apple_music: "Apple Music"
};

type ScrobbleState = {
  lastfm_username?: string;
  lastfm_connected?: boolean;
  listenbrainz_username?: string;
  listenbrainz_connected?: boolean;
  presence_enabled?: boolean;
  lastfm_configured?: boolean;
};

export function ConnectedServicesPage() {
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const q = useQuery({ queryKey: ["me-providers"], queryFn: () => api.get<any[]>("/api/v1/me/providers") });
  const sc = useQuery({ queryKey: ["me-scrobble"], queryFn: () => api.get<ScrobbleState>("/api/v1/me/scrobble") });
  const [appleTok, setAppleTok] = useState("");
  const [lfUser, setLfUser] = useState("");
  const [lfPass, setLfPass] = useState("");
  const [lbToken, setLbToken] = useState("");
  const [lbUser, setLbUser] = useState("");

  useEffect(() => {
    if (params.get("connected")) toast.success(`Connected ${labels[params.get("connected")!] || params.get("connected")}`);
    if (params.get("error")) toast.error("Could not connect provider");
  }, [params]);

  useEffect(() => {
    const ch = params.get("discord_link");
    if (!ch) return;
    api
      .post("/api/v1/me/discord/link", { challenge: ch })
      .then(() => {
        toast.success("Discord account linked");
        qc.invalidateQueries({ queryKey: ["me"] });
      })
      .catch(() => toast.error("Could not complete Discord link"));
  }, [params, qc]);

  useEffect(() => {
    ensureDiscordPresence();
  }, []);

  const presence = !!sc.data?.presence_enabled;

  return (
    <div className="max-w-2xl">
      <PageHeader
        title="Connected Services"
        description="Link playlist providers to import and keep playlists in sync. SoundDock never downloads their audio."
      />
      <div className="space-y-3">
        {(q.data || []).map((p) => (
          <article key={p.provider} className="rounded-xl border border-border bg-surface-1 p-4">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <Link2 className="h-4 w-4 text-accent" />
                  <h2 className="font-semibold">{labels[p.provider] || p.provider}</h2>
                  {p.connected ? <Badge tone="success">Connected</Badge> : <Badge>Not connected</Badge>}
                </div>
                {p.account_name && <p className="mt-1 text-sm text-muted">{p.account_name}</p>}
                {p.scopes?.length ? <p className="text-xs text-subtle">{p.scopes.join(", ")}</p> : null}
                {p.last_error ? <p className="text-xs text-destructive">{p.last_error}</p> : null}
              </div>
              <div className="flex gap-2">
                {p.provider === "apple_music" && p.enabled && p.users_may_connect && !p.connected && null}
                {p.enabled && p.users_may_connect && p.provider !== "apple_music" && (
                  <Button
                    size="sm"
                    onClick={async () => {
                      const r = await api.post<{ url: string }>(`/api/v1/me/providers/${p.provider}/connect`);
                      window.location.href = r.url;
                    }}
                  >
                    {p.connected ? "Reconnect" : "Connect"}
                  </Button>
                )}
                {p.connected && (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={async () => {
                      await api.del(`/api/v1/me/providers/${p.provider}`);
                      toast.success("Disconnected. Playlists kept, media untouched");
                      qc.invalidateQueries({ queryKey: ["me-providers"] });
                    }}
                  >
                    Disconnect
                  </Button>
                )}
              </div>
            </div>
            {p.provider === "apple_music" && p.enabled && p.users_may_connect && (
              <form
                className="mt-3 space-y-2"
                onSubmit={async (e) => {
                  e.preventDefault();
                  await api.post("/api/v1/me/providers/apple_music/connect", { music_user_token: appleTok, name: "Apple Music" });
                  toast.success("Apple Music connected");
                  setAppleTok("");
                  qc.invalidateQueries({ queryKey: ["me-providers"] });
                }}
              >
                <Field label="Music User Token" hint="From MusicKit JS after you authorize this instance. Stored encrypted on the server, never in the browser.">
                  <Textarea value={appleTok} onChange={(e) => setAppleTok(e.target.value)} rows={3} />
                </Field>
                <Button type="submit" size="sm">Save token</Button>
              </form>
            )}
            {!p.enabled && <p className="mt-2 text-xs text-subtle">An administrator must enable this provider.</p>}
          </article>
        ))}

        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <Radio className="h-4 w-4 text-accent" />
                <h2 className="font-semibold">Last.fm</h2>
                {sc.data?.lastfm_connected ? <Badge tone="success">Connected</Badge> : <Badge>Not connected</Badge>}
              </div>
              {sc.data?.lastfm_username && <p className="mt-1 text-sm text-muted">{sc.data.lastfm_username}</p>}
              <p className="mt-1 text-xs text-subtle">Scrobble SoundDock plays. Import writes listen history with source import only.</p>
            </div>
            {sc.data?.lastfm_connected && (
              <Button
                size="sm"
                variant="ghost"
                onClick={async () => {
                  await api.put("/api/v1/me/scrobble", { lastfm_disconnect: true });
                  toast.success("Last.fm disconnected");
                  qc.invalidateQueries({ queryKey: ["me-scrobble"] });
                }}
              >
                Disconnect
              </Button>
            )}
          </div>
          {!sc.data?.lastfm_connected && (
            <form
              className="mt-3 space-y-2"
              onSubmit={async (e) => {
                e.preventDefault();
                await api.put("/api/v1/me/scrobble", { lastfm_username: lfUser, lastfm_password: lfPass });
                toast.success("Last.fm connected");
                setLfPass("");
                qc.invalidateQueries({ queryKey: ["me-scrobble"] });
              }}
            >
              <Field label="Username" hint={sc.data?.lastfm_configured ? undefined : "An administrator must set SD_LASTFM_API_KEY and SD_LASTFM_API_SECRET."}>
                <Input value={lfUser} onChange={(e) => setLfUser(e.target.value)} autoComplete="username" />
              </Field>
              <Field label="Password" hint="Used once to create a Last.fm session. Never sent to Discord.">
                <Input type="password" value={lfPass} onChange={(e) => setLfPass(e.target.value)} autoComplete="current-password" />
              </Field>
              <Button type="submit" size="sm">Connect Last.fm</Button>
            </form>
          )}
          {sc.data?.lastfm_connected && (
            <Button
              className="mt-3"
              size="sm"
              variant="outline"
              onClick={async () => {
                const r = await api.post<{ imported: number; skipped: number }>("/api/v1/me/scrobble/import", { provider: "lastfm" });
                toast.success(`Imported ${r.imported} listens (${r.skipped} unmatched)`);
              }}
            >
              Import Last.fm history
            </Button>
          )}
        </article>

        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <Radio className="h-4 w-4 text-accent" />
                <h2 className="font-semibold">ListenBrainz</h2>
                {sc.data?.listenbrainz_connected ? <Badge tone="success">Connected</Badge> : <Badge>Not connected</Badge>}
              </div>
              {sc.data?.listenbrainz_username && <p className="mt-1 text-sm text-muted">{sc.data.listenbrainz_username}</p>}
              <p className="mt-1 text-xs text-subtle">Submit listens to ListenBrainz. Import uses source import only.</p>
            </div>
            {sc.data?.listenbrainz_connected && (
              <Button
                size="sm"
                variant="ghost"
                onClick={async () => {
                  await api.put("/api/v1/me/scrobble", { listenbrainz_disconnect: true });
                  toast.success("ListenBrainz disconnected");
                  qc.invalidateQueries({ queryKey: ["me-scrobble"] });
                }}
              >
                Disconnect
              </Button>
            )}
          </div>
          {!sc.data?.listenbrainz_connected && (
            <form
              className="mt-3 space-y-2"
              onSubmit={async (e) => {
                e.preventDefault();
                await api.put("/api/v1/me/scrobble", { listenbrainz_token: lbToken, listenbrainz_username: lbUser });
                toast.success("ListenBrainz connected");
                setLbToken("");
                qc.invalidateQueries({ queryKey: ["me-scrobble"] });
              }}
            >
              <Field label="User token" hint="From listenbrainz.org profile. Stored encrypted on the server.">
                <Input type="password" value={lbToken} onChange={(e) => setLbToken(e.target.value)} />
              </Field>
              <Field label="Username" hint="Your ListenBrainz user name (for history import).">
                <Input value={lbUser} onChange={(e) => setLbUser(e.target.value)} />
              </Field>
              <Button type="submit" size="sm">Save token</Button>
            </form>
          )}
          {sc.data?.listenbrainz_connected && (
            <Button
              className="mt-3"
              size="sm"
              variant="outline"
              onClick={async () => {
                const r = await api.post<{ imported: number; skipped: number }>("/api/v1/me/scrobble/import", { provider: "listenbrainz" });
                toast.success(`Imported ${r.imported} listens (${r.skipped} unmatched)`);
              }}
            >
              Import ListenBrainz history
            </Button>
          )}
        </article>

        <article className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <Radio className="h-4 w-4 text-accent" />
                <h2 className="font-semibold">Discord Rich Presence</h2>
                {presence ? <Badge tone="success">On</Badge> : <Badge>Off</Badge>}
              </div>
              <p className="mt-1 text-xs text-subtle">
                Shows the web player as Listening on the Discord desktop client (localhost RPC). Does not replace guild voice playback.
              </p>
            </div>
            <Switch
              checked={presence}
              onCheckedChange={async (v) => {
                await api.put("/api/v1/me/scrobble", { presence_enabled: v });
                setDiscordPresenceEnabled(v);
                qc.invalidateQueries({ queryKey: ["me-scrobble"] });
              }}
            />
          </div>
        </article>
      </div>
    </div>
  );
}
