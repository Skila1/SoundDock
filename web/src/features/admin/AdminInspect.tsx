import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/empty";
import { formatDuration, relativeTime } from "@/lib/utils";

type InspectDump = {
  generated_at?: string;
  counts?: Record<string, number>;
  playback?: { sessions?: SessionRow[] };
  discord?: {
    settings?: { enabled?: boolean; gateway?: string; commands?: string; last_error?: string };
    runtime?: RuntimeRow[];
    voice?: VoiceRow[];
    playback_errors?: ErrorRow[];
  };
  errors?: { items?: ErrorRow[] };
  jobs?: { failed?: ErrorRow[] };
  acquisition?: { intents?: ErrorRow[]; holds?: HoldRow[] };
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

type ErrorRow = {
  id?: string;
  source?: string;
  class?: string;
  type?: string;
  message?: string;
  last_error?: string;
  error?: string;
  at?: string;
  created_at?: string;
  status?: string;
  title?: string;
};

type HoldRow = {
  id?: string;
  title?: string;
  kind?: string;
  active?: boolean;
  holder_id?: string;
};

const sources = ["", "oplog", "job", "job_attempt", "discord_playback", "discord_gateway", "acquisition", "scan_file", "webhook", "external_account", "external_playlist", "external_sync"];

export function AdminInspect() {
  const [source, setSource] = useState("");
  const [qtext, setQtext] = useState("");
  const dump = useQuery({ queryKey: ["admin-inspect"], queryFn: () => api.get<InspectDump>("/api/v1/admin/inspect"), refetchInterval: 8000 });
  const errors = useQuery({
    queryKey: ["admin-errors", source, qtext],
    queryFn: () => api.get<{ items?: ErrorRow[] }>(`/api/v1/admin/errors?limit=80&source=${encodeURIComponent(source)}&q=${encodeURIComponent(qtext)}`),
    refetchInterval: 8000
  });
  const d = dump.data || {};
  const counts = d.counts || {};
  const sessions = d.playback?.sessions || [];
  const runtime = d.discord?.runtime || [];
  const voice = d.discord?.voice || [];
  const items = errors.data?.items || d.errors?.items || [];

  return (
    <div>
      <PageHeader
        title="Inspect"
        description="Live playback, Discord bind, and every error table. An admin API key can read the same JSON from /api/v1/admin/inspect."
        actions={
          <Button variant="secondary" disabled={dump.isFetching} onClick={() => { dump.refetch(); errors.refetch(); }}>
            {dump.isFetching ? "Refreshing…" : "Refresh"}
          </Button>
        }
      />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={counts.playing ? "warning" : "neutral"}>{counts.playing || 0} playing</Badge>
        <Badge tone={counts.discord_connected ? "success" : "neutral"}>{counts.discord_connected || 0} Discord VC</Badge>
        <Badge tone={counts.failed_jobs ? "danger" : "neutral"}>{counts.failed_jobs || 0} failed jobs</Badge>
        <Badge tone={counts.acquisition_failed ? "danger" : "neutral"}>{counts.acquisition_failed || 0} acquire failed</Badge>
        <Badge>{counts.playback_sessions || 0} sessions</Badge>
        <Badge>{counts.media_holds || 0} holds</Badge>
        {d.discord?.settings?.last_error && <Badge tone="danger">Discord gateway error</Badge>}
      </div>

      <h3 className="mb-2 font-semibold">Playback sessions</h3>
      {sessions.length === 0 ? (
        <p className="mb-6 text-sm text-muted">No playback sessions.</p>
      ) : (
        <ul className="mb-6 divide-y divide-border rounded-xl border border-border bg-surface-1">
          {sessions.map((sess) => (
            <li key={sess.id} className="px-4 py-3">
              <div className="flex flex-wrap items-start justify-between gap-2">
                <div>
                  <div className="font-medium">
                    {sess.current_title || sess.current_track_id || "No track"}{" "}
                    <span className="text-sm font-normal text-muted">
                      {sess.username || sess.display_name || sess.kind}
                    </span>
                  </div>
                  <div className="text-sm text-muted">
                    {sess.status} · {sess.output_pref}/{sess.renderer_kind}
                    {sess.queue_len != null ? ` · ${sess.queue_len} queued` : ""}
                    {sess.duration_ms ? ` · ${formatDuration(sess.position_ms)} / ${formatDuration(sess.duration_ms)}` : ""}
                    {sess.current_has_file === false && sess.current_track_id ? " · missing file" : ""}
                  </div>
                  {sess.discord && (
                    <div className="text-xs text-subtle">
                      Discord {sess.discord.connected ? "connected" : "idle"} {sess.discord.guild_id || ""} {sess.discord.last_disconnect_reason || ""}
                    </div>
                  )}
                </div>
                <div className="flex flex-wrap gap-2">
                  <Badge tone={sess.status === "playing" ? "warning" : "neutral"}>{sess.status || "idle"}</Badge>
                  {sess.current_has_file === false && sess.current_track_id && <Badge tone="danger">no file</Badge>}
                </div>
              </div>
              <div className="mt-1 font-mono text-[11px] text-subtle">{sess.id}</div>
            </li>
          ))}
        </ul>
      )}

      <h3 className="mb-2 font-semibold">Discord</h3>
      <div className="mb-2 flex flex-wrap gap-2">
        <Badge tone={d.discord?.settings?.enabled ? "success" : "neutral"}>{d.discord?.settings?.enabled ? "Enabled" : "Disabled"}</Badge>
        <Badge>{d.discord?.settings?.gateway || "gateway unknown"}</Badge>
        <Badge>{d.discord?.settings?.commands || "commands unknown"}</Badge>
      </div>
      {d.discord?.settings?.last_error && <p className="mb-3 text-sm text-destructive">{d.discord.settings.last_error}</p>}
      <ul className="mb-4 space-y-2">
        {runtime.map((r) => (
          <li key={r.guild_id} className="rounded-lg border border-border px-4 py-3 text-sm">
            <div className="font-medium">{r.guild_name || r.guild_id}</div>
            <div className="text-muted">
              {r.connected ? "connected" : r.last_disconnect_reason || r.reason || "not in voice"}
              {r.session_id ? ` · session ${r.session_id}` : " · no session"}
              {r.status ? ` · ${r.status}` : ""}
              {r.binding_revision != null ? ` · bind ${r.binding_revision}` : ""}
            </div>
            {r.voice_channel_id && <div className="text-xs text-subtle">channel {r.voice_channel_id}</div>}
          </li>
        ))}
      </ul>
      {voice.length > 0 && (
        <ul className="mb-6 text-sm text-muted">
          {voice.map((v) => (
            <li key={`${v.discord_user_id}-${v.guild_id}`}>
              {v.username || v.display_name || v.discord_user_id} in {v.channel_id || "no channel"}
            </li>
          ))}
        </ul>
      )}

      <h3 className="mb-2 font-semibold">Errors</h3>
      <form
        className="mb-3 flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const fd = new FormData(e.currentTarget);
          setQtext(String(fd.get("q") || ""));
        }}
      >
        <input name="q" className="rounded-md border border-border bg-surface-1 px-3 py-2 text-sm" placeholder="Search errors" defaultValue={qtext} />
        <select className="rounded-md border border-border bg-surface-1 px-3 py-2 text-sm" value={source} onChange={(e) => setSource(e.target.value)}>
          {sources.map((src) => (
            <option key={src || "all"} value={src}>{src || "All sources"}</option>
          ))}
        </select>
        <Button type="submit" variant="secondary">Filter</Button>
      </form>
      <ul className="space-y-2">
        {items.map((err, i) => (
          <li key={err.id || `${err.source}-${err.at}-${i}`} className="rounded-lg border border-border px-4 py-3 text-sm">
            <div className="font-medium">{err.source || err.type || err.class || "error"}</div>
            <div className="text-destructive">{err.message || err.last_error || err.error}</div>
            <div className="text-xs text-subtle">{[err.class, err.status, relativeTime(err.at || err.created_at)].filter(Boolean).join(" · ")}</div>
          </li>
        ))}
        {items.length === 0 && <li className="text-sm text-muted">No errors in this filter.</li>}
      </ul>
    </div>
  );
}
