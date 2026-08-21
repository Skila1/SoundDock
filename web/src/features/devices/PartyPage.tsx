import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Users } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { relativeTime } from "@/lib/utils";
import { toast } from "sonner";
import type { SearchHit } from "@/types/api";

type PartyMember = { user_id: string; role: string };
type PartyVote = { track_id?: string; user_id?: string; created_at?: string };
type PartyState = {
  session_id: string;
  enabled: boolean;
  host_user_id?: string;
  expires_at?: string | null;
  members: PartyMember[];
  votes: PartyVote[];
};

export function PartyPage() {
  const qc = useQueryClient();
  const [hours, setHours] = useState("1");
  const [q, setQ] = useState("");
  const party = useQuery({
    queryKey: ["me-party"],
    queryFn: () => api.get<PartyState>("/api/v1/me/party"),
    retry: false
  });
  const search = useQuery({
    queryKey: ["party-search", q],
    enabled: q.trim().length > 1,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&type=track&limit=8`)
  });

  const p = party.data;
  const tracks = (search.data?.results || []).filter((h) => h.type === "track");

  async function setEnabled(enabled: boolean) {
    try {
      const expires_in_seconds = Math.max(60, Math.round(Number(hours) * 3600) || 3600);
      await api.post("/api/v1/me/party", enabled ? { enabled: true, expires_in_seconds } : { enabled: false });
      toast.success(enabled ? "Party started" : "Party ended");
      qc.invalidateQueries({ queryKey: ["me-party"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Party update failed");
    }
  }

  async function vote(track_id: string) {
    try {
      await api.post("/api/v1/me/party/votes", { track_id });
      toast.success("Vote sent");
      qc.invalidateQueries({ queryKey: ["me-party"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Vote failed");
    }
  }

  return (
    <div className="max-w-2xl">
      <PageHeader title="Party" description="Listen together. Host, members, and votes use the shared party API." />
      {party.isError && !p && (
        <EmptyState icon={Users} title="Party unavailable" description="Start a party to invite others onto this playback session." />
      )}
      <div className="space-y-4 rounded-xl border border-border bg-surface-1 p-5">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Users className="h-4 w-4 text-accent" />
            <h2 className="font-semibold">Listening party</h2>
            <Badge tone={p?.enabled ? "success" : "neutral"}>{p?.enabled ? "On" : "Off"}</Badge>
          </div>
          {p?.enabled ? (
            <Button size="sm" variant="ghost" onClick={() => setEnabled(false)}>End party</Button>
          ) : (
            <Button size="sm" onClick={() => setEnabled(true)}>Start party</Button>
          )}
        </div>
        {!p?.enabled && (
          <Field label="Duration (hours)">
            <Input type="number" min={1} max={12} value={hours} onChange={(e) => setHours(e.target.value)} className="max-w-[8rem]" />
          </Field>
        )}
        {p?.enabled && (
          <p className="text-sm text-muted">
            {p.expires_at ? `Expires ${relativeTime(p.expires_at)}` : "No expiry"}
            {p.host_user_id ? ` · host ${p.host_user_id}` : ""}
          </p>
        )}
        <div>
          <h3 className="mb-2 text-sm font-semibold">Members ({p?.members?.length || 0})</h3>
          <ul className="space-y-1 text-sm">
            {(p?.members || []).map((m) => (
              <li key={m.user_id} className="flex items-center gap-2">
                <span className="truncate">{m.user_id}</span>
                <Badge>{m.role}</Badge>
              </li>
            ))}
          </ul>
        </div>
        <div>
          <h3 className="mb-2 text-sm font-semibold">Votes ({p?.votes?.length || 0})</h3>
          <ul className="mb-3 space-y-1 text-sm text-muted">
            {(p?.votes || []).map((v, i) => (
              <li key={`${v.track_id || i}-${v.user_id || i}`}>{v.track_id || "track"}{v.user_id ? ` · ${v.user_id}` : ""}</li>
            ))}
          </ul>
          {p?.enabled && (
            <>
              <Field label="Vote for a track">
                <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search library" />
              </Field>
              <ul className="mt-2 space-y-1">
                {tracks.map((t) => (
                  <li key={t.id} className="flex items-center justify-between gap-2 rounded-lg border border-border px-3 py-2 text-sm">
                    <span className="truncate">{t.title}{t.artist ? ` · ${t.artist}` : ""}</span>
                    <Button size="sm" variant="outline" onClick={() => vote(t.id)}>Vote</Button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
