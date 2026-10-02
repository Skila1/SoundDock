import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { formatDuration, relativeTime } from "@/lib/utils";

type InspectDump = {
  generated_at?: string;
  counts?: Record<string, number>;
  playback?: { sessions?: SessionRow[] };
  discord?: {
    settings?: { enabled?: boolean; gateway?: string; commands?: string; last_error?: string };
    runtime?: RuntimeRow[];
    voice?: VoiceRow[];
  };
};

type SessionRow = {
  id?: string;
  kind?: string;
  username?: string;
  display_name?: string;
  status?: string;
  output_pref?: string;
  renderer_kind?: string;
  current_title?: string;
  current_track_id?: string;
  current_has_file?: boolean;
  position_ms?: number;
  duration_ms?: number;
  queue_len?: number;
  updated_at?: string;
  discord?: { guild_id?: string; voice_channel_id?: string; connected?: boolean; last_disconnect_reason?: string };
};

type RuntimeRow = {
  guild_id?: string;
  guild_name?: string;
  voice_channel_id?: string;
  session_id?: string;
  connected?: boolean;
  last_disconnect_reason?: string;
  reason?: string;
  status?: string;
  current_track_id?: string;
  binding_revision?: number;
};

type VoiceRow = {
  discord_user_id?: string;
  username?: string;
  display_name?: string;
  guild_id?: string;
  channel_id?: string;
};



export function LiveTab() {
  const dump = useQuery({ queryKey: ["admin-inspect"], queryFn: () => api.get<InspectDump>("/api/v1/admin/inspect"), refetchInterval: 5000 });
  const d = dump.data || {};
  const counts = d.counts || {};
  const sessions = d.playback?.sessions || [];
  const runtime = d.discord?.runtime || [];
  const voice = d.discord?.voice || [];

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Badge tone={counts.playing ? "warning" : "neutral"}>{counts.playing || 0} playing</Badge>
        <Badge>{counts.playback_sessions || 0} sessions</Badge>
        <Badge tone={counts.discord_connected ? "success" : "neutral"}>{counts.discord_connected || 0} in Discord voice</Badge>
        <Badge tone={counts.failed_jobs ? "danger" : "neutral"}>{counts.failed_jobs || 0} failed jobs</Badge>
        <Badge tone={counts.acquisition_failed ? "danger" : "neutral"}>{counts.acquisition_failed || 0} failed downloads</Badge>
        <Badge>{counts.media_holds || 0} files in use</Badge>
        {d.discord?.settings?.last_error && <Badge tone="danger">Discord bot error</Badge>}
        <span className="ml-auto text-xs text-subtle">Refreshes every 5 seconds</span>
      </div>

      <h3 className="mb-2 font-semibold">Playback sessions</h3>
      {sessions.length === 0 ? (
        <p className="mb-6 text-sm text-muted">Nobody is listening right now.</p>
      ) : (
        <ul className="mb-6 divide-y divide-border rounded-xl border border-border bg-surface-1">
          {sessions.map((sess) => (
            <li key={sess.id} className="px-4 py-3">
              <div className="flex flex-wrap items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="font-medium">
                    {sess.current_title || "Nothing queued"}{" "}
                    <span className="text-sm font-normal text-muted">{sess.username || sess.display_name || sess.kind}</span>
                  </div>
                  <div className="text-sm text-muted">
                    {[
                      sess.renderer_kind || sess.output_pref,
                      sess.queue_len != null ? `${sess.queue_len} queued` : "",
                      sess.duration_ms ? `${formatDuration(sess.position_ms)} / ${formatDuration(sess.duration_ms)}` : "",
                      sess.updated_at ? `updated ${relativeTime(sess.updated_at)}` : ""
                    ].filter(Boolean).join(" · ")}
                  </div>
                  {sess.discord && (
                    <div className="text-xs text-subtle">
                      Discord {sess.discord.connected ? "connected" : "idle"}{sess.discord.last_disconnect_reason ? ` · ${sess.discord.last_disconnect_reason}` : ""}
                    </div>
                  )}
                </div>
                <div className="flex flex-wrap gap-2">
                  <Badge tone={sess.status === "playing" ? "warning" : "neutral"}>{sess.status || "idle"}</Badge>
                  {sess.current_has_file === false && sess.current_track_id && <Badge tone="danger">file missing</Badge>}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}

      <h3 className="mb-2 font-semibold">Discord voice</h3>
      <div className="mb-2 flex flex-wrap gap-2">
        <Badge tone={d.discord?.settings?.enabled ? "success" : "neutral"}>{d.discord?.settings?.enabled ? "Bot enabled" : "Bot disabled"}</Badge>
        {d.discord?.settings?.gateway && <Badge>Gateway: {d.discord.settings.gateway}</Badge>}
        {d.discord?.settings?.commands && <Badge>Commands: {d.discord.settings.commands}</Badge>}
      </div>
      {d.discord?.settings?.last_error && <p className="mb-3 text-sm text-destructive">{d.discord.settings.last_error}</p>}
      {runtime.length === 0 ? (
        <p className="text-sm text-muted">The bot is not in any voice channel.</p>
      ) : (
        <ul className="mb-4 space-y-2">
          {runtime.map((r) => (
            <li key={r.guild_id} className="rounded-lg border border-border px-4 py-3 text-sm">
              <div className="font-medium">{r.guild_name || r.guild_id}</div>
              <div className="text-muted">
                {r.connected ? "Connected" : r.last_disconnect_reason || r.reason || "Not in voice"}
                {r.status ? ` · ${r.status}` : ""}
              </div>
            </li>
          ))}
        </ul>
      )}
      {voice.length > 0 && (
        <>
          <h4 className="mb-1 mt-4 text-sm font-medium">People in voice</h4>
          <ul className="text-sm text-muted">
            {voice.map((v) => (
              <li key={`${v.discord_user_id}-${v.guild_id}`}>{v.display_name || v.username || v.discord_user_id}</li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
