import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Headphones, Radio } from "lucide-react";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { cn, formatDuration } from "@/lib/utils";

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
};

type RuntimeRow = { guild_id?: string; guild_name?: string; session_id?: string; connected?: boolean; status?: string };
type VoiceRow = { discord_user_id?: string; username?: string; display_name?: string; guild_id?: string };

type InspectDump = {
  playback?: { sessions?: SessionRow[] };
  discord?: { settings?: { enabled?: boolean }; runtime?: RuntimeRow[]; voice?: VoiceRow[] };
};

/** Who is listening right now, on the web and in Discord voice. */
export function NowPlaying() {
  const q = useQuery({ queryKey: ["admin-inspect"], queryFn: () => api.get<InspectDump>("/api/v1/admin/inspect"), refetchInterval: 5000 });
  const all = q.data?.playback?.sessions || [];
  const sessions = all.filter((s) => s.kind !== "discord_guild" && s.current_title);
  const runtime = (q.data?.discord?.runtime || []).filter((r) => r.connected);
  const voice = q.data?.discord?.voice || [];
  const botOn = !!q.data?.discord?.settings?.enabled;

  return (
    <section className="grid gap-3 md:grid-cols-2">
      <Panel icon={Headphones} title="Listening on the web" count={sessions.length}>
        {sessions.length === 0 ? (
          <Empty loading={q.isLoading}>Nobody is listening right now.</Empty>
        ) : (
          <ul className="divide-y divide-border">
            {sessions.map((s) => {
              const pct = s.duration_ms ? Math.min(100, ((s.position_ms || 0) / s.duration_ms) * 100) : 0;
              return (
                <li key={s.id} className="py-2.5 first:pt-0 last:pb-0">
                  <div className="flex items-center justify-between gap-2">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">{s.current_title}</div>
                      <div className="truncate text-xs text-subtle">
                        {[s.display_name || s.username || s.kind, s.renderer_kind || s.output_pref, s.queue_len ? `${s.queue_len} queued` : ""].filter(Boolean).join(" · ")}
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {s.current_has_file === false && s.current_track_id && <Badge tone="danger">File missing</Badge>}
                      <Badge tone={s.status === "playing" ? "accent" : "neutral"}>{s.status || "idle"}</Badge>
                    </div>
                  </div>
                  {s.duration_ms ? (
                    <div className="mt-1.5 flex items-center gap-2 text-[11px] text-subtle">
                      <div className="h-1 flex-1 overflow-hidden rounded-full bg-surface-3">
                        <div className={cn("h-full rounded-full", s.status === "playing" ? "bg-accent" : "bg-muted")} style={{ width: `${pct}%` }} />
                      </div>
                      <span className="tabular shrink-0">{formatDuration(s.position_ms)} / {formatDuration(s.duration_ms)}</span>
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ul>
        )}
      </Panel>

      <Panel icon={Radio} title="Discord voice" count={runtime.length}>
        {!botOn && !q.isLoading ? (
          <Empty>The Discord bot is off.</Empty>
        ) : runtime.length === 0 ? (
          <Empty loading={q.isLoading}>The bot is not in any voice channel.</Empty>
        ) : (
          <ul className="divide-y divide-border">
            {runtime.map((r) => {
              const people = voice.filter((v) => v.guild_id === r.guild_id);
              const title = all.find((s) => s.id === r.session_id)?.current_title;
              return (
                <li key={r.guild_id} className="py-2.5 first:pt-0 last:pb-0">
                  <div className="flex items-center justify-between gap-2">
                    <div className="min-w-0 truncate text-sm font-medium">{r.guild_name || r.guild_id}</div>
                    {r.status && <Badge tone={r.status === "playing" ? "accent" : "neutral"}>{r.status}</Badge>}
                  </div>
                  {title && <div className="truncate text-sm">{title}</div>}
                  <div className="truncate text-xs text-subtle">
                    {people.length ? people.map((v) => v.display_name || v.username || v.discord_user_id).join(", ") : "Nobody else in the channel"}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Panel>
    </section>
  );
}

function Panel({ icon: Icon, title, count, children }: { icon: typeof Headphones; title: string; count: number; children: ReactNode }) {
  return (
    <div className="min-w-0 rounded-2xl border border-border bg-surface-1 p-4 shadow-sm">
      <div className="mb-3 flex items-center gap-2">
        <Icon className="h-4 w-4 text-accent" />
        <h2 className="text-sm font-semibold">{title}</h2>
        {count > 0 && <span className="tabular rounded-full bg-accent/15 px-2 text-[11px] font-semibold leading-5 text-accent">{count}</span>}
      </div>
      {children}
    </div>
  );
}

function Empty({ children, loading }: { children: ReactNode; loading?: boolean }) {
  return <p className="text-sm text-muted">{loading ? "Loading…" : children}</p>;
}
