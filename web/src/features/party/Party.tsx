import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Navigate, useSearchParams } from "react-router-dom";
import {
  Copy,
  Crown,
  ListPlus,
  Pause,
  Play,
  SkipBack,
  SkipForward,
  ThumbsUp,
  UserMinus,
  Users,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { cn, relativeTime } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { LocalSearch, useDebounced } from "@/components/media/LibraryToolbar";
import { SettingSwitch } from "@/features/admin/adminUi";
import {
  useParty,
  type PartyPermissions,
  type PartyState,
  type PartyVote,
} from "@/stores/party";
import type { SearchHit, User } from "@/types/api";

const PERMS: {
  key: keyof PartyPermissions;
  label: string;
  description: string;
}[] = [
  {
    key: "add",
    label: "Add songs",
    description: "Guests' play and queue buttons add to your queue.",
  },
  {
    key: "skip",
    label: "Skip and go back",
    description: "Guests can move to the next or previous song.",
  },
  {
    key: "pause",
    label: "Pause and resume",
    description: "Guests can pause and resume playback.",
  },
  {
    key: "vote",
    label: "Vote for songs",
    description: "Guests can vote for what plays next.",
  },
];

/** Keeps the shared party state fresh for the header button and the player. */
function usePartySync() {
  const set = useParty((s) => s.set);
  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api.get<User>("/api/v1/me"),
  });
  const party = useQuery({
    queryKey: ["me-party"],
    queryFn: () => api.get<PartyState>("/api/v1/me/party"),
    retry: false,
    refetchInterval: (q) => (q.state.data?.enabled ? 5_000 : 30_000),
  });
  useEffect(() => {
    set({ myId: me.data?.id ?? null });
  }, [me.data?.id, set]);
  useEffect(() => {
    if (party.data) set({ party: party.data });
  }, [party.data, set]);
}

/** Header button: lights up while a party is live and opens the party panel. */
export function PartyButton() {
  usePartySync();
  const { party, open, set } = useParty();
  const live = !!party?.enabled;
  const count = party?.members.length ?? 0;
  return (
    <>
      <Button
        size="icon"
        variant="ghost"
        aria-label={live ? `Party, ${count} in it` : "Party"}
        title={live ? "Listening party is on" : "Start a listening party"}
        className={cn("relative", live && "text-accent")}
        onClick={() => set({ open: true })}
      >
        <Users />
        {live && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-bold text-accent-foreground">
            {count}
          </span>
        )}
      </Button>
      <Sheet open={open} onOpenChange={(v) => set({ open: v })}>
        <SheetContent
          title="Listening party"
          className="w-[min(460px,94vw)] overflow-y-auto"
        >
          <PartyPanel />
        </SheetContent>
      </Sheet>
    </>
  );
}

function PartyPanel() {
  const { party: p, myId, joinId, set } = useParty();
  const [hours, setHours] = useState("2");
  const [term, setTerm] = useState("");
  const q = useDebounced(term.trim(), 250);
  const search = useQuery({
    queryKey: ["party-search", q],
    enabled: q.length > 1,
    queryFn: () =>
      api.get<{ results: SearchHit[] }>(
        `/api/v1/search?q=${encodeURIComponent(q)}&type=track&limit=8`,
      ),
  });
  const isHost = !!p?.enabled && !!myId && p.host_user_id === myId;
  const myRole = p?.members.find((m) => m.user_id === myId)?.role;
  const perms = p?.permissions || {
    add: true,
    vote: true,
    skip: false,
    pause: false,
  };
  const can = (k: keyof PartyPermissions) =>
    isHost || myRole === "dj" || perms[k];
  const tracks = (search.data?.results || []).filter((h) => h.type === "track");
  const body = (extra: Record<string, unknown> = {}) =>
    p?.session_id && !isHost ? { session_id: p.session_id, ...extra } : extra;

  const run = async (
    fn: () => Promise<PartyState | undefined | null>,
    ok?: string,
  ) => {
    try {
      const next = await fn();
      if (next) set({ party: next });
      if (ok) toast.success(ok);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Party update failed");
    }
  };
  const start = () =>
    run(
      () =>
        api.post<PartyState>("/api/v1/me/party", {
          enabled: true,
          expires_in_seconds: Math.max(
            60,
            Math.round(Number(hours) * 3600) || 7200,
          ),
        }),
      "Party started with your current queue",
    );
  const stop = () =>
    run(
      () =>
        api.post<PartyState>("/api/v1/me/party", {
          enabled: false,
          ...(isHost ? {} : { session_id: p?.session_id }),
        }),
      isHost ? "Party ended" : "You left the party",
    );
  const join = () =>
    run(async () => {
      const st = await api.post<PartyState>("/api/v1/me/party", {
        enabled: true,
        session_id: joinId,
      });
      set({ joinId: "" });
      return st;
    }, "Joined. Play and queue buttons now add to the party.");
  const inviteLink = p?.session_id
    ? `${location.origin}/profile/party?join=${p.session_id}`
    : "";

  if (joinId && !p?.enabled) {
    return (
      <div className="space-y-3">
        <p className="text-sm text-muted">
          You are invited to a listening party. Once you join, the play and
          queue buttons everywhere add songs to it.
        </p>
        <Button onClick={() => void join()}>
          <Users /> Join party
        </Button>
      </div>
    );
  }
  if (!p?.enabled) {
    return (
      <div className="space-y-4">
        <p className="text-sm text-muted">
          Listen together on your playback. Your current queue carries over, and
          guests add songs from anywhere in SoundDock.
        </p>
        <div className="flex items-end gap-3">
          <label className="text-sm">
            <span className="mb-1 block text-xs font-medium text-muted">
              Length (hours)
            </span>
            <Input
              type="number"
              min={1}
              max={12}
              value={hours}
              onChange={(e) => setHours(e.target.value)}
              className="w-24"
            />
          </label>
          <Button onClick={() => void start()}>
            <Users /> Start party
          </Button>
        </div>
      </div>
    );
  }
  return (
    <div className="space-y-5">
      <section className="rounded-xl border border-border bg-surface-2/50 p-3">
        <div className="flex items-center justify-between gap-2 text-xs text-muted">
          <span className="flex items-center gap-1.5">
            <Badge tone="success">Live</Badge>
            {p.expires_at ? `Ends ${relativeTime(p.expires_at)}` : null}
          </span>
          <Button size="sm" variant="ghost" onClick={() => void stop()}>
            {isHost ? "End party" : "Leave"}
          </Button>
        </div>
        <div className="mt-2 flex items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="truncate font-semibold">
              {p.current?.title || "Nothing playing"}
            </div>
            <div className="truncate text-sm text-muted">
              {p.current?.artist}
            </div>
          </div>
          {!isHost && (
            <div className="flex items-center gap-1">
              {can("skip") && (
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label="Previous"
                  onClick={() =>
                    void run(() =>
                      api.post<PartyState>(
                        "/api/v1/me/party/control",
                        body({ action: "previous" }),
                      ),
                    )
                  }
                >
                  <SkipBack />
                </Button>
              )}
              {can("pause") && (
                <Button
                  size="icon"
                  aria-label={
                    p.current?.status === "playing" ? "Pause" : "Play"
                  }
                  onClick={() =>
                    void run(() =>
                      api.post<PartyState>(
                        "/api/v1/me/party/control",
                        body({
                          action:
                            p.current?.status === "playing"
                              ? "pause"
                              : "resume",
                        }),
                      ),
                    )
                  }
                >
                  {p.current?.status === "playing" ? (
                    <Pause className="fill-current" />
                  ) : (
                    <Play className="fill-current" />
                  )}
                </Button>
              )}
              {can("skip") && (
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label="Skip"
                  onClick={() =>
                    void run(() =>
                      api.post<PartyState>(
                        "/api/v1/me/party/control",
                        body({ action: "skip" }),
                      ),
                    )
                  }
                >
                  <SkipForward />
                </Button>
              )}
            </div>
          )}
        </div>
        {!isHost && can("add") && (
          <p className="mt-2 text-xs text-subtle">
            Play or queue any song in SoundDock and it goes into this party.
          </p>
        )}
      </section>

      {isHost && inviteLink && (
        <Button
          variant="secondary"
          className="w-full"
          onClick={() =>
            navigator.clipboard
              .writeText(inviteLink)
              .then(() => toast.success("Invite link copied"))
          }
        >
          <Copy /> Copy invite link
        </Button>
      )}

      {!isHost && can("vote") && (
        <section>
          <h3 className="mb-2 text-sm font-semibold">Vote for songs</h3>
          <LocalSearch
            value={term}
            onChange={setTerm}
            placeholder="Search the library"
          />
          <ul className="mt-2 space-y-1">
            {tracks.map((t) => (
              <li
                key={t.id}
                className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 hover:bg-surface-2/70"
              >
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">
                    {t.title}
                  </span>
                  <span className="block truncate text-xs text-muted">
                    {t.artist}
                  </span>
                </span>
                <span className="flex shrink-0 gap-1">
                  {can("add") && (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() =>
                        void run(
                          () =>
                            api.post<PartyState>(
                              "/api/v1/me/party/queue",
                              body({ track_ids: [t.id] }),
                            ),
                          "Added to the party queue",
                        )
                      }
                    >
                      <ListPlus /> Queue
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() =>
                      void run(
                        () =>
                          api.post<PartyState>(
                            "/api/v1/me/party/votes",
                            body({ track_id: t.id }),
                          ),
                        "Vote sent",
                      )
                    }
                  >
                    <ThumbsUp /> Vote
                  </Button>
                </span>
              </li>
            ))}
            {q.length > 1 && !search.isLoading && !tracks.length && (
              <li className="px-2 text-sm text-muted">No songs match.</li>
            )}
          </ul>
        </section>
      )}

      <section>
        <h3 className="mb-2 text-sm font-semibold">
          People ({p.members.length})
        </h3>
        <ul className="space-y-1">
          {p.members.map((m) => (
            <li
              key={m.user_id}
              className="flex items-center gap-2 rounded-lg px-1 py-1.5"
            >
              <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-accent/15 text-xs font-bold text-accent">
                {(m.name || "?").slice(0, 1).toUpperCase()}
              </span>
              <span className="min-w-0 flex-1 truncate text-sm">
                {m.name || "Listener"}
                {m.user_id === myId && (
                  <span className="text-subtle"> (you)</span>
                )}
              </span>
              {m.role === "host" ? (
                <Badge tone="accent">
                  <Crown className="h-3 w-3" /> Host
                </Badge>
              ) : isHost ? (
                <>
                  <Select
                    className="h-8 w-[92px] text-xs"
                    value={m.role === "dj" ? "dj" : "guest"}
                    onValueChange={(v) =>
                      void run(() =>
                        api.post<PartyState>(
                          `/api/v1/me/party/members/${m.user_id}`,
                          { role: v },
                        ),
                      )
                    }
                    options={[
                      { value: "guest", label: "Guest" },
                      { value: "dj", label: "DJ" },
                    ]}
                  />
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-8 w-8"
                    aria-label={`Remove ${m.name || "member"}`}
                    onClick={() =>
                      void run(() =>
                        api.del<PartyState>(
                          `/api/v1/me/party/members/${m.user_id}`,
                        ),
                      )
                    }
                  >
                    <UserMinus className="h-4 w-4" />
                  </Button>
                </>
              ) : (
                <Badge>{m.role === "dj" ? "DJ" : "Guest"}</Badge>
              )}
            </li>
          ))}
        </ul>
      </section>

      {p.votes.length > 0 && (
        <section>
          <h3 className="mb-2 text-sm font-semibold">
            Votes ({p.votes.length})
          </h3>
          <VoteTally votes={p.votes} />
        </section>
      )}

      {isHost && (
        <section>
          <h3 className="mb-1 text-sm font-semibold">What guests can do</h3>
          <p className="mb-2 text-xs text-muted">
            DJs and you can always do everything.
          </p>
          {PERMS.map((perm) => (
            <SettingSwitch
              key={perm.key}
              label={perm.label}
              description={perm.description}
              checked={!!perms[perm.key]}
              onChange={(v) =>
                void run(() =>
                  api.put<PartyState>("/api/v1/me/party/permissions", {
                    permissions: { ...perms, [perm.key]: v },
                  }),
                )
              }
            />
          ))}
        </section>
      )}
    </div>
  );
}

function VoteTally({ votes }: { votes: PartyVote[] }) {
  const counts = new Map<string, number>();
  for (const v of votes)
    if (v.track_id) counts.set(v.track_id, (counts.get(v.track_id) || 0) + 1);
  const ids = [...counts.keys()];
  const titles = useQuery({
    queryKey: ["party-vote-titles", ids.join(",")],
    enabled: ids.length > 0,
    queryFn: async () => {
      const out: Record<string, string> = {};
      await Promise.all(
        ids.slice(0, 20).map(async (id) => {
          try {
            const t = await api.get<{ title: string }>(`/api/v1/tracks/${id}`);
            out[id] = t.title;
          } catch {
            /* removed track */
          }
        }),
      );
      return out;
    },
  });
  return (
    <ol className="space-y-1 text-sm">
      {[...counts.entries()]
        .sort((a, b) => b[1] - a[1])
        .map(([id, n]) => (
          <li key={id} className="flex items-center justify-between gap-2">
            <span className="truncate">{titles.data?.[id] || "Song"}</span>
            <Badge tone="accent">{n}</Badge>
          </li>
        ))}
    </ol>
  );
}

/** Old /profile/party links (and invites) open the party panel instead of a page. */
export function PartyLink() {
  const set = useParty((s) => s.set);
  const [params] = useSearchParams();
  const join = params.get("join") || "";
  useEffect(() => {
    set({ open: true, joinId: join });
  }, [join, set]);
  return <Navigate to="/" replace />;
}
