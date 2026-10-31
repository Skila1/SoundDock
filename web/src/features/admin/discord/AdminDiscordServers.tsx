import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Ban, Check, Copy, DoorOpen, LogOut, RefreshCcw, ShieldCheck, Undo2, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { cn, relativeTime } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Card, RowMenu, SettingSwitch, errorMessage } from "../adminUi";
import { guildsKey } from "../iam/iamShared";

export type DiscordServer = {
  id: string;
  name: string;
  icon_url: string;
  owner_id: string;
  owner_name: string;
  member_count: number;
  bot_admin: boolean;
  permissions: { key: string; label: string; granted: boolean }[];
  info_refreshed_at: string | null;
  joined_at: string | null;
  left_at: string | null;
  banned_at: string | null;
  ban_reason: string;
  banned_by: string;
  status: "connected" | "left" | "banned";
  enabled: boolean;
  queue_mode: "shared" | "personal";
  library_mode: "all" | "selected";
  library_ids: string[];
  queue_limit: number;
  explicit_policy: string;
  in_voice: boolean;
  bot_online?: boolean;
};

type Library = { id: string; name: string };

type ActivityItem = {
  id: string;
  at: string;
  kind: "admin" | "bot" | "command" | "error";
  level: string;
  action: string;
  message: string;
  actor?: string;
  error?: string;
  details?: Record<string, unknown> | null;
};

export const serversKey = ["discord-servers"] as const;
const serverKey = (id: string) => ["discord-server", id] as const;

function ServerIcon({ server, size = "md" }: { server: Pick<DiscordServer, "name" | "icon_url">; size?: "md" | "lg" }) {
  const cls = size === "lg" ? "h-14 w-14 text-lg" : "h-10 w-10 text-sm";
  if (server.icon_url) return <img src={server.icon_url} alt="" className={cn("shrink-0 rounded-xl object-cover", cls)} />;
  const letters = (server.name || "?").split(/\s+/).map((w) => w[0]).join("").slice(0, 2).toUpperCase();
  return <span className={cn("grid shrink-0 place-items-center rounded-xl bg-surface-2 font-semibold text-muted", cls)}>{letters}</span>;
}

function StatusBadge({ server }: { server: DiscordServer }) {
  if (server.status === "banned") return <Badge tone="danger">Banned</Badge>;
  if (server.status === "left") return <Badge>Bot not in server</Badge>;
  if (!server.enabled) return <Badge tone="warning">Turned off</Badge>;
  if (server.in_voice) return <Badge tone="success">In voice</Badge>;
  return <Badge tone="accent">Connected</Badge>;
}

function libraryLabel(server: DiscordServer, libs: Library[]) {
  if (server.library_mode === "all") return "All libraries";
  if (!server.library_ids.length) return "No libraries";
  const names = server.library_ids.map((id) => libs.find((l) => l.id === id)?.name).filter(Boolean);
  return names.length ? names.join(", ") : `${server.library_ids.length} libraries`;
}

/** Discord > Servers: every server the bot is in, has left, or is banned from. */
export function AdminDiscordServers() {
  const q = useQuery({ queryKey: serversKey, queryFn: () => api.get<{ servers: DiscordServer[]; bot_online: boolean }>("/api/v1/admin/discord/servers") });
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const servers = q.data?.servers || [];
  const groups = [
    { title: "Servers", rows: servers.filter((s) => s.status === "connected"), empty: "Invite the bot to a server to get started." },
    { title: "Bot not in server", rows: servers.filter((s) => s.status === "left") },
    { title: "Banned", rows: servers.filter((s) => s.status === "banned") }
  ];
  return (
    <div>
      <PageHeader title="Discord servers" description="Every server the bot is in. Open one to set its queue and libraries, see its activity, or remove the bot." />
      {q.data && !q.data.bot_online && (
        <p className="mb-4 rounded-lg border border-warning/40 px-3 py-2 text-sm text-warning">
          The bot is offline. Settings still save, but owner details, leaving and bans wait until it reconnects. <Link className="underline" to="/admin/discord/bot">Bot settings</Link>
        </p>
      )}
      <div className="space-y-5">
        {groups.map((g) =>
          g.rows.length || g.empty ? (
            <Card key={g.title} title={`${g.title}${g.rows.length ? ` (${g.rows.length})` : ""}`}>
              {!g.rows.length && <p className="text-sm text-muted">{q.isLoading ? "Loading..." : g.empty}</p>}
              <ul className="divide-y divide-border">
                {g.rows.map((s) => (
                  <li key={s.id}>
                    <Link to={`/admin/discord/servers/${s.id}`} className="-mx-2 flex items-center gap-3 rounded-lg px-2 py-2.5 hover:bg-surface-2/60">
                      <ServerIcon server={s} />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2 font-medium">
                          <span className="truncate">{s.name || s.id}</span>
                          <StatusBadge server={s} />
                        </div>
                        <div className="truncate text-xs text-muted">
                          {s.status === "banned"
                            ? `Banned ${relativeTime(s.banned_at)}${s.banned_by ? ` by ${s.banned_by}` : ""}${s.ban_reason ? ` · ${s.ban_reason}` : ""}`
                            : `${s.owner_name ? `Owner ${s.owner_name}` : "Owner unknown"}${s.member_count ? ` · ${s.member_count} members` : ""} · ${s.queue_mode === "shared" ? "Shared queue" : "Personal queues"} · ${libraryLabel(s, libs.data || [])}`}
                        </div>
                      </div>
                    </Link>
                  </li>
                ))}
              </ul>
            </Card>
          ) : null
        )}
      </div>
    </div>
  );
}

function CopyValue({ label, value }: { label: string; value: string }) {
  if (!value) return <span className="text-muted">Unknown</span>;
  return (
    <button
      type="button"
      className="inline-flex max-w-full items-center gap-1.5 truncate font-mono text-xs text-muted hover:text-foreground"
      onClick={() => navigator.clipboard.writeText(value).then(() => toast.success(`${label} copied`))}
      aria-label={`Copy ${label}`}
    >
      {value}
      <Copy className="h-3 w-3 shrink-0" />
    </button>
  );
}

/** Discord > Servers > one server. */
export function AdminDiscordServer() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: serverKey(id), queryFn: () => api.get<DiscordServer>(`/api/v1/admin/discord/servers/${id}`), enabled: !!id });
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const [confirm, setConfirm] = useState<null | "leave" | "unban">(null);
  const [banOpen, setBanOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const apply = (s: DiscordServer) => {
    qc.setQueryData(serverKey(id), s);
    qc.invalidateQueries({ queryKey: serversKey });
    qc.invalidateQueries({ queryKey: guildsKey });
    qc.invalidateQueries({ queryKey: ["discord-server-activity", id] });
  };
  const run = async (fn: () => Promise<DiscordServer>, ok: string, fail: string) => {
    setBusy(true);
    try {
      apply(await fn());
      toast.success(ok);
    } catch (e) {
      toast.error(errorMessage(e, fail));
    } finally {
      setBusy(false);
    }
  };
  const patch = (body: Partial<Pick<DiscordServer, "enabled" | "queue_mode" | "library_mode" | "library_ids" | "queue_limit" | "explicit_policy">>, ok = "Saved") =>
    run(() => api.patch<DiscordServer>(`/api/v1/admin/discord/servers/${id}`, body), ok, "Could not save");
  const post = (path: string, ok: string, fail: string, body?: unknown) => run(() => api.post<DiscordServer>(`/api/v1/admin/discord/servers/${id}/${path}`, body), ok, fail);

  if (q.isError) {
    return (
      <div>
        <BackLink />
        <p className="text-sm text-muted">This server is not known to SoundDock.</p>
      </div>
    );
  }
  const s = q.data;
  if (!s) return <div className="h-48 animate-pulse rounded-xl bg-surface-2" />;
  const offline = s.bot_online === false;
  const inServer = s.status === "connected";

  return (
    <div>
      <BackLink />
      <div className="mb-7 flex flex-wrap items-center gap-4">
        <ServerIcon server={s} size="lg" />
        <div className="min-w-0 flex-1">
          <h1 className="flex flex-wrap items-center gap-2 text-2xl font-semibold tracking-tight">
            <span className="truncate">{s.name || s.id}</span>
            <StatusBadge server={s} />
          </h1>
          <p className="text-sm text-muted">
            {s.joined_at ? `Bot added ${relativeTime(s.joined_at)}` : "Bot join time unknown"}
            {s.left_at && s.status !== "banned" ? ` · left ${relativeTime(s.left_at)}` : ""}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {inServer && (
            <Button size="sm" variant="secondary" disabled={busy || offline} onClick={() => post("refresh", "Server details refreshed", "Could not refresh")}>
              <RefreshCcw /> Refresh
            </Button>
          )}
          <RowMenu
            actions={[
              ...(s.in_voice ? [{ label: "Disconnect from voice", icon: LogOut, onSelect: () => void post("disconnect", "Disconnected from voice", "Could not disconnect") }] : []),
              ...(inServer ? [{ label: "Leave server", icon: DoorOpen, disabled: offline, onSelect: () => setConfirm("leave") }] : []),
              s.status === "banned"
                ? { label: "Unban server", icon: Undo2, onSelect: () => setConfirm("unban") }
                : { label: "Ban server", icon: Ban, destructive: true, separatorBefore: true, onSelect: () => setBanOpen(true) }
            ]}
          />
        </div>
      </div>

      {s.status === "banned" && (
        <p className="mb-5 rounded-lg border border-destructive/40 px-3 py-2 text-sm text-destructive">
          Banned {relativeTime(s.banned_at)}{s.banned_by ? ` by ${s.banned_by}` : ""}{s.ban_reason ? `: ${s.ban_reason}` : ""}. If someone invites the bot back, it leaves straight away.
        </p>
      )}

      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Server" description={s.info_refreshed_at ? `Details from Discord, ${relativeTime(s.info_refreshed_at)}` : "Details arrive when the bot sees the server."}>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted">Owner</dt>
            <dd className="min-w-0">{s.owner_name || <span className="text-muted">Unknown</span>}</dd>
            <dt className="text-muted">Owner ID</dt>
            <dd className="min-w-0"><CopyValue label="Owner ID" value={s.owner_id} /></dd>
            <dt className="text-muted">Server ID</dt>
            <dd className="min-w-0"><CopyValue label="Server ID" value={s.id} /></dd>
            <dt className="text-muted">Members</dt>
            <dd>{s.member_count || <span className="text-muted">Unknown</span>}</dd>
          </dl>
        </Card>

        <Card
          title="Bot permissions"
          description={s.bot_admin ? "The bot is an administrator here, so it has every permission." : "What the bot can do in this server."}
          actions={s.bot_admin ? <Badge tone="success"><ShieldCheck className="h-3 w-3" /> Administrator</Badge> : undefined}
        >
          {s.info_refreshed_at ? (
            <ul className="grid gap-1.5 text-sm sm:grid-cols-2">
              {s.permissions.filter((p) => p.key !== "administrator").map((p) => (
                <li key={p.key} className={cn("flex items-center gap-2", !p.granted && "text-muted")}>
                  {p.granted ? <Check className="h-4 w-4 text-success" /> : <X className="h-4 w-4 text-destructive" />}
                  {p.label}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted">Not read yet. Refresh once the bot is online.</p>
          )}
        </Card>

        <PlaybackCard server={s} busy={busy} onPatch={patch} />
        <LibrariesCard server={s} libs={libs.data || []} busy={busy} onPatch={patch} />
      </div>

      <ServerActivity id={id} />

      <ConfirmDialog
        open={confirm === "leave"}
        onOpenChange={(v) => !v && setConfirm(null)}
        title={`Leave ${s.name || "this server"}?`}
        description="The bot leaves the server and stops playing there. Anyone with permission can invite it back. Ban the server instead to keep it out."
        confirmLabel="Leave server"
        destructive
        onConfirm={() => void post("leave", "The bot left the server", "Could not leave the server")}
      />
      <ConfirmDialog
        open={confirm === "unban"}
        onOpenChange={(v) => !v && setConfirm(null)}
        title={`Unban ${s.name || "this server"}?`}
        description="The bot can be invited back to this server. It does not rejoin by itself."
        confirmLabel="Unban"
        onConfirm={() => void post("unban", "Server unbanned", "Could not unban")}
      />
      <BanDialog
        open={banOpen}
        name={s.name}
        onOpenChange={setBanOpen}
        onBan={(reason) => {
          setBanOpen(false);
          void post("ban", "Server banned", "Could not ban the server", { reason });
        }}
      />
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/admin/discord/servers" className="mb-4 inline-flex items-center gap-1.5 text-sm text-muted hover:text-foreground">
      <ArrowLeft className="h-4 w-4" /> All servers
    </Link>
  );
}

function BanDialog({ open, name, onOpenChange, onBan }: { open: boolean; name: string; onOpenChange: (v: boolean) => void; onBan: (reason: string) => void }) {
  const [reason, setReason] = useState("");
  useEffect(() => {
    if (open) setReason("");
  }, [open]);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={`Ban ${name || "this server"}?`}>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            onBan(reason.trim());
          }}
        >
          <p className="text-sm text-muted">
            The bot leaves now, and leaves again straight away whenever someone invites it back. Discord cannot stop the invite itself, so you may see it join and leave in the activity log.
          </p>
          <Field label="Reason" hint="Only shown to admins.">
            <Input value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} placeholder="Optional" />
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" variant="destructive"><Ban /> Ban server</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

type PatchFn = (body: Partial<Pick<DiscordServer, "enabled" | "queue_mode" | "library_mode" | "library_ids" | "queue_limit" | "explicit_policy">>, ok?: string) => Promise<void>;

function PlaybackCard({ server, busy, onPatch }: { server: DiscordServer; busy: boolean; onPatch: PatchFn }) {
  const [limit, setLimit] = useState(String(server.queue_limit));
  useEffect(() => setLimit(String(server.queue_limit)), [server.queue_limit]);
  const limitNum = Number(limit);
  const limitDirty = limit !== String(server.queue_limit) && Number.isInteger(limitNum) && limitNum >= 1 && limitNum <= 10000;
  return (
    <Card title="Playback">
      <div className="space-y-4">
        <SettingSwitch
          label="Bot active in this server"
          description="Off ignores slash commands here and stops web listening from joining its voice channels."
          checked={server.enabled}
          disabled={busy}
          onChange={(on) => void onPatch({ enabled: on }, on ? "Server turned on" : "Server turned off")}
        />
        <Field label="Queue" hint={server.queue_mode === "shared" ? "Everyone in the server adds to and controls one queue. Personal queues on the website stay separate." : "The bot plays the queue of whoever started it, as before."}>
          <Select
            value={server.queue_mode}
            onValueChange={(v) => void onPatch({ queue_mode: v as DiscordServer["queue_mode"] }, "Queue mode saved")}
            options={[
              { value: "shared", label: "One shared queue for the server" },
              { value: "personal", label: "Personal queues" }
            ]}
          />
        </Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Queue limit">
            <div className="flex gap-2">
              <Input type="number" min={1} max={10000} value={limit} onChange={(e) => setLimit(e.target.value)} />
              {limitDirty && <Button size="sm" disabled={busy} onClick={() => void onPatch({ queue_limit: limitNum }, "Queue limit saved")}>Save</Button>}
            </div>
          </Field>
          <Field label="Explicit songs">
            <Select
              value={server.explicit_policy === "deny" ? "deny" : "allow"}
              onValueChange={(v) => void onPatch({ explicit_policy: v }, "Saved")}
              options={[
                { value: "allow", label: "Allowed" },
                { value: "deny", label: "Blocked" }
              ]}
            />
          </Field>
        </div>
      </div>
    </Card>
  );
}

function LibrariesCard({ server, libs, busy, onPatch }: { server: DiscordServer; libs: Library[]; busy: boolean; onPatch: PatchFn }) {
  const [picked, setPicked] = useState<Set<string>>(new Set(server.library_ids));
  useEffect(() => setPicked(new Set(server.library_ids)), [server.library_ids]);
  const dirty = useMemo(() => picked.size !== server.library_ids.length || server.library_ids.some((l) => !picked.has(l)), [picked, server.library_ids]);
  const toggle = (id: string) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  return (
    <Card title="Libraries" description="Which libraries slash commands and web listening in this server can use. Songs stay in their library either way; this only hides them here.">
      <div className="space-y-4">
        <Select
          value={server.library_mode}
          onValueChange={(v) =>
            void onPatch(
              v === "selected" ? { library_mode: "selected", library_ids: server.library_ids.length ? server.library_ids : libs.map((l) => l.id) } : { library_mode: "all" },
              v === "selected" ? "Only picked libraries now" : "All libraries now"
            )
          }
          options={[
            { value: "all", label: "All libraries" },
            { value: "selected", label: "Only the libraries picked below" }
          ]}
        />
        {server.library_mode === "selected" && (
          <>
            <ul className="space-y-1">
              {libs.map((l) => (
                <li key={l.id}>
                  <label className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-surface-2/60">
                    <input type="checkbox" className="h-4 w-4 accent-[var(--sd-accent)]" checked={picked.has(l.id)} onChange={() => toggle(l.id)} />
                    {l.name}
                  </label>
                </li>
              ))}
            </ul>
            {!picked.size && <p className="text-xs text-warning">With no libraries picked, this server can only play songs fetched from YouTube.</p>}
            {dirty && (
              <div className="flex gap-2">
                <Button size="sm" disabled={busy} onClick={() => void onPatch({ library_ids: [...picked] }, "Libraries saved")}>Save libraries</Button>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setPicked(new Set(server.library_ids))}>Discard</Button>
              </div>
            )}
          </>
        )}
      </div>
    </Card>
  );
}

const kindLabels: Record<ActivityItem["kind"], string> = { admin: "Admin", bot: "Bot", command: "Command", error: "Error" };
const kindTones: Record<ActivityItem["kind"], "neutral" | "accent" | "danger" | "warning"> = { admin: "accent", bot: "neutral", command: "neutral", error: "danger" };

function detailText(item: ActivityItem) {
  const d = item.details || {};
  const parts: string[] = [];
  if (d.options && typeof d.options === "object") {
    parts.push(Object.entries(d.options as Record<string, unknown>).map(([k, v]) => `${k}: ${String(v)}`).join(", "));
  }
  for (const [k, v] of Object.entries(d)) {
    if (["options", "guild_name", "channel_id", "discord_user_id"].includes(k) || v == null || v === "") continue;
    parts.push(`${k.replace(/_/g, " ")}: ${Array.isArray(v) ? v.join(", ") : typeof v === "object" ? JSON.stringify(v) : String(v)}`);
  }
  if (item.error) parts.push(item.error);
  return parts.filter(Boolean).join(" · ");
}

function ServerActivity({ id }: { id: string }) {
  const [kind, setKind] = useState<"all" | ActivityItem["kind"]>("all");
  const q = useInfiniteQuery({
    queryKey: ["discord-server-activity", id],
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      api.get<{ items: ActivityItem[]; next_before: string }>(`/api/v1/admin/discord/servers/${id}/activity?limit=100${pageParam ? `&before=${encodeURIComponent(pageParam)}` : ""}`),
    getNextPageParam: (last) => last.next_before || undefined,
    refetchInterval: 30_000
  });
  const items = (q.data?.pages || []).flatMap((p) => p.items).filter((i) => kind === "all" || i.kind === kind);
  return (
    <Card
      className="mt-5"
      title="Activity"
      description="Admin changes, bot joins and leaves, slash commands and playback errors in this server."
      actions={
        <Select
          className="h-9 w-36"
          value={kind}
          onValueChange={(v) => setKind(v as typeof kind)}
          options={[
            { value: "all", label: "Everything" },
            { value: "admin", label: "Admin" },
            { value: "command", label: "Commands" },
            { value: "bot", label: "Bot" },
            { value: "error", label: "Errors" }
          ]}
        />
      }
    >
      {!items.length && <p className="text-sm text-muted">{q.isLoading ? "Loading..." : "Nothing yet."}</p>}
      <ul className="divide-y divide-border">
        {items.map((i) => (
          <li key={i.id} className="flex gap-3 py-2.5 text-sm">
            <span className="w-20 shrink-0 text-xs text-muted" title={new Date(i.at).toLocaleString()}>{relativeTime(i.at)}</span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone={i.level === "error" ? "danger" : i.level === "warn" ? "warning" : kindTones[i.kind]}>{kindLabels[i.kind]}</Badge>
                <span className="font-medium">{i.message}</span>
                {i.actor && <span className="text-muted">by {i.actor}</span>}
              </div>
              {detailText(i) && <div className="mt-0.5 break-words text-xs text-muted">{detailText(i)}</div>}
            </div>
          </li>
        ))}
      </ul>
      {q.hasNextPage && (
        <Button className="mt-3" size="sm" variant="secondary" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {q.isFetchingNextPage ? "Loading..." : "Load older"}
        </Button>
      )}
    </Card>
  );
}
