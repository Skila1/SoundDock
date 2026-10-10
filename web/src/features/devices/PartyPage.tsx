import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Crown, Headphones, ListPlus, Pause, Play, SkipBack, SkipForward, ThumbsUp, UserMinus, Users } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { relativeTime } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { Select } from "@/components/ui/select";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { LocalSearch, useDebounced } from "@/components/media/LibraryToolbar";
import { Card, SettingSwitch } from "@/features/admin/adminUi";
import type { SearchHit, User } from "@/types/api";

type Permissions = { add: boolean; vote: boolean; skip: boolean; pause: boolean };
type PartyMember = { user_id: string; role: "host" | "dj" | "guest" | string; name?: string };
type PartyVote = { track_id?: string; user_id?: string; created_at?: string };
type PartyState = {
  session_id: string;
  enabled: boolean;
  host_user_id?: string | null;
  expires_at?: string | null;
  members: PartyMember[];
  votes: PartyVote[];
  permissions?: Permissions;
  current?: { track_id: string; title: string; artist: string; status: string } | null;
};

const PERMS: { key: keyof Permissions; label: string; description: string }[] = [
  { key: "add", label: "Add songs to the queue", description: "Guests can queue songs on your playback." },
  { key: "skip", label: "Skip and go back", description: "Guests can move to the next or previous song." },
  { key: "pause", label: "Pause and resume", description: "Guests can pause and resume playback." },
  { key: "vote", label: "Vote for songs", description: "Guests can vote for what plays next." }
];

/** Listen together: the host decides what guests may do; DJs can do it all. */
export function PartyPage() {
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const joinId = params.get("join") || "";
  const [hours, setHours] = useState("2");
  const [term, setTerm] = useState("");
  const q = useDebounced(term.trim(), 250);
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const party = useQuery({
    queryKey: ["me-party"],
    queryFn: () => api.get<PartyState>("/api/v1/me/party"),
    retry: false,
    refetchInterval: 10_000
  });
  const search = useQuery({
    queryKey: ["party-search", q],
    enabled: q.length > 1,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&type=track&limit=8`)
  });

  const p = party.data;
  const myId = me.data?.id;
  const isHost = !!p?.enabled && !!myId && p.host_user_id === myId;
  const myRole = p?.members.find((m) => m.user_id === myId)?.role;
  const perms = p?.permissions || { add: true, vote: true, skip: false, pause: false };
  const can = (k: keyof Permissions) => isHost || myRole === "dj" || perms[k];
  const tracks = (search.data?.results || []).filter((h) => h.type === "track");
  const set = (next: PartyState) => qc.setQueryData(["me-party"], next);
  const body = (extra: Record<string, unknown> = {}) => (p?.session_id && !isHost ? { session_id: p.session_id, ...extra } : extra);

  const run = async (fn: () => Promise<PartyState | undefined | null>, ok?: string) => {
    try {
      const next = await fn();
      if (next) set(next);
      if (ok) toast.success(ok);
      void qc.invalidateQueries({ queryKey: ["me-party"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Party update failed");
    }
  };

  const start = () =>
    run(() => api.post<PartyState>("/api/v1/me/party", { enabled: true, expires_in_seconds: Math.max(60, Math.round(Number(hours) * 3600) || 7200) }), "Party started");
  const stop = () => run(() => api.post<PartyState>("/api/v1/me/party", { enabled: false, ...(isHost ? {} : { session_id: p?.session_id }) }), isHost ? "Party ended" : "You left the party");
  const join = () =>
    run(async () => {
      const st = await api.post<PartyState>("/api/v1/me/party", { enabled: true, session_id: joinId });
      setParams({}, { replace: true });
      return st;
    }, "Joined the party");
  const inviteLink = p?.session_id ? `${location.origin}/profile/party?join=${p.session_id}` : "";

  return (
    <div className="max-w-4xl">
      <PageHeader
        title="Party"
        description="Listen together on one playback. You decide what guests can do, and can make trusted members DJs."
        actions={
          p?.enabled ? (
            <Button variant="ghost" onClick={() => void stop()}>{isHost ? "End party" : "Leave party"}</Button>
          ) : undefined
        }
      />

      {joinId && !p?.enabled && (
        <Card className="mb-6" title="You're invited to a party" description="Join to see what's playing and add songs if the host allows it.">
          <Button onClick={() => void join()}><Users /> Join party</Button>
        </Card>
      )}

      {!p?.enabled ? (
        <Card title="Start a party" description="Guests join with your invite link while it's active.">
          <div className="flex flex-wrap items-end gap-3">
            <Field label="Length (hours)">
              <Input type="number" min={1} max={12} value={hours} onChange={(e) => setHours(e.target.value)} className="w-28" />
            </Field>
            <Button onClick={() => void start()}><Users /> Start party</Button>
          </div>
        </Card>
      ) : (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
          <div className="space-y-6">
            <Card
              title={
                <span className="flex items-center gap-2">
                  <Headphones className="h-4 w-4 text-accent" /> Now playing
                  <Badge tone="success">Live</Badge>
                </span>
              }
              description={p.expires_at ? `Ends ${relativeTime(p.expires_at)}` : undefined}
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate text-lg font-semibold">{p.current?.title || "Nothing playing"}</div>
                  <div className="truncate text-sm text-muted">{p.current?.artist}</div>
                </div>
                {!isHost && (
                  <div className="flex items-center gap-1">
                    {can("skip") && (
                      <Button size="icon" variant="ghost" aria-label="Previous" onClick={() => void run(() => api.post<PartyState>("/api/v1/me/party/control", body({ action: "previous" })))}>
                        <SkipBack />
                      </Button>
                    )}
                    {can("pause") && (
                      <Button
                        size="icon"
                        aria-label={p.current?.status === "playing" ? "Pause" : "Play"}
                        onClick={() => void run(() => api.post<PartyState>("/api/v1/me/party/control", body({ action: p.current?.status === "playing" ? "pause" : "resume" })))}
                      >
                        {p.current?.status === "playing" ? <Pause className="fill-current" /> : <Play className="fill-current" />}
                      </Button>
                    )}
                    {can("skip") && (
                      <Button size="icon" variant="ghost" aria-label="Skip" onClick={() => void run(() => api.post<PartyState>("/api/v1/me/party/control", body({ action: "skip" })))}>
                        <SkipForward />
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </Card>

            {(can("add") || can("vote")) && (
              <Card title={can("add") ? "Add songs" : "Vote for songs"} description={can("add") ? "Songs go into the party queue." : "The host sees the most-voted songs."}>
                <LocalSearch value={term} onChange={setTerm} placeholder="Search the library" />
                <ul className="mt-3 space-y-1">
                  {tracks.map((t) => (
                    <li key={t.id} className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 hover:bg-surface-2/70">
                      <span className="min-w-0">
                        <span className="block truncate text-sm font-medium">{t.title}</span>
                        <span className="block truncate text-xs text-muted">{t.artist}</span>
                      </span>
                      <span className="flex shrink-0 gap-1">
                        {can("add") && (
                          <Button size="sm" variant="secondary" onClick={() => void run(() => api.post<PartyState>("/api/v1/me/party/queue", body({ track_ids: [t.id] })), "Added to the party queue")}>
                            <ListPlus /> Queue
                          </Button>
                        )}
                        {can("vote") && (
                          <Button size="sm" variant="ghost" onClick={() => void run(() => api.post<PartyState>("/api/v1/me/party/votes", body({ track_id: t.id })), "Vote sent")}>
                            <ThumbsUp /> Vote
                          </Button>
                        )}
                      </span>
                    </li>
                  ))}
                  {q.length > 1 && !search.isLoading && !tracks.length && <li className="px-2 text-sm text-muted">No songs match.</li>}
                </ul>
              </Card>
            )}

            {isHost && (
              <Card title="What guests can do" description="DJs and you can always do everything.">
                {PERMS.map((perm) => (
                  <SettingSwitch
                    key={perm.key}
                    label={perm.label}
                    description={perm.description}
                    checked={!!perms[perm.key]}
                    onChange={(v) => void run(() => api.put<PartyState>("/api/v1/me/party/permissions", { permissions: { ...perms, [perm.key]: v } }))}
                  />
                ))}
              </Card>
            )}
          </div>

          <div className="space-y-6">
            {isHost && inviteLink && (
              <Card title="Invite" description="Anyone signed in to SoundDock can join with this link.">
                <Button
                  variant="secondary"
                  className="w-full"
                  onClick={() => navigator.clipboard.writeText(inviteLink).then(() => toast.success("Invite link copied"))}
                >
                  <Copy /> Copy invite link
                </Button>
              </Card>
            )}
            <Card title={`People (${p.members.length})`}>
              {!p.members.length ? (
                <EmptyState icon={Users} title="Nobody has joined yet." />
              ) : (
                <ul className="space-y-1">
                  {p.members.map((m) => (
                    <li key={m.user_id} className="flex items-center gap-2 rounded-lg px-1 py-1.5">
                      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-accent/15 text-xs font-bold text-accent">
                        {(m.name || "?").slice(0, 1).toUpperCase()}
                      </span>
                      <span className="min-w-0 flex-1 truncate text-sm">
                        {m.name || "Listener"}
                        {m.user_id === myId && <span className="text-subtle"> (you)</span>}
                      </span>
                      {m.role === "host" ? (
                        <Badge tone="accent"><Crown className="h-3 w-3" /> Host</Badge>
                      ) : isHost ? (
                        <>
                          <Select
                            className="h-8 w-[92px] text-xs"
                            value={m.role === "dj" ? "dj" : "guest"}
                            onValueChange={(v) => void run(() => api.post<PartyState>(`/api/v1/me/party/members/${m.user_id}`, { role: v }))}
                            options={[
                              { value: "guest", label: "Guest" },
                              { value: "dj", label: "DJ" }
                            ]}
                          />
                          <Button size="icon" variant="ghost" className="h-8 w-8" aria-label={`Remove ${m.name || "member"}`} onClick={() => void run(() => api.del<PartyState>(`/api/v1/me/party/members/${m.user_id}`))}>
                            <UserMinus className="h-4 w-4" />
                          </Button>
                        </>
                      ) : (
                        <Badge>{m.role === "dj" ? "DJ" : "Guest"}</Badge>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </Card>
            {p.votes.length > 0 && (
              <Card title={`Votes (${p.votes.length})`}>
                <VoteTally votes={p.votes} />
              </Card>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function VoteTally({ votes }: { votes: PartyVote[] }) {
  const counts = new Map<string, number>();
  for (const v of votes) if (v.track_id) counts.set(v.track_id, (counts.get(v.track_id) || 0) + 1);
  const ids = [...counts.keys()];
  const titles = useQuery({
    queryKey: ["party-vote-titles", ids.join(",")],
    enabled: ids.length > 0,
    queryFn: async () => {
      const out: Record<string, string> = {};
      await Promise.all(ids.slice(0, 20).map(async (id) => {
        try {
          const t = await api.get<{ title: string }>(`/api/v1/tracks/${id}`);
          out[id] = t.title;
        } catch {
          /* removed track */
        }
      }));
      return out;
    }
  });
  return (
    <ol className="space-y-1 text-sm">
      {[...counts.entries()].sort((a, b) => b[1] - a[1]).map(([id, n]) => (
        <li key={id} className="flex items-center justify-between gap-2">
          <span className="truncate">{titles.data?.[id] || "Song"}</span>
          <Badge tone="accent">{n}</Badge>
        </li>
      ))}
    </ol>
  );
}
