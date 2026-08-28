import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, GitMerge } from "lucide-react";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { PageHeader, EmptyState } from "@/components/ui/empty";
import { formatDuration } from "@/lib/utils";
import { toast } from "sonner";

type ReviewTrack = {
  id: string;
  title: string;
  artist: string;
  duration_ms: number;
};

type ReviewGroup = {
  id: string;
  group_id?: string | null;
  status: string;
  reason: string;
  tracks: ReviewTrack[];
};

function reasonLabel(reason?: string) {
  if (reason === "content_hash") return "Same file hash";
  if (reason === "artist_title") return "Same artist and title";
  return reason || "Duplicate";
}

export function AdminDuplicateReview() {
  const qc = useQueryClient();
  const [winnerByGroup, setWinnerByGroup] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const q = useQuery({
    queryKey: ["admin-duplicate-review"],
    queryFn: () => api.get<ReviewGroup[]>("/api/v1/admin/duplicate-review")
  });

  const groups = Array.isArray(q.data) ? q.data : [];

  async function mergeGroup(g: ReviewGroup) {
    const winner = winnerByGroup[g.id] || g.tracks[0]?.id;
    if (!winner) return;
    const losers = g.tracks.map((t) => t.id).filter((id) => id !== winner);
    if (losers.length === 0) {
      toast.error("Pick a winner; every other track in the group is discarded");
      return;
    }
    setBusy(g.id);
    try {
      await api.post(`/api/v1/admin/duplicate-review/${g.id}/merge`, { winner_id: winner, loser_ids: losers });
      toast.success("Duplicates merged");
      await qc.invalidateQueries({ queryKey: ["admin-duplicate-review"] });
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 409) {
        toast.error("A discarded track is currently playing. Stop playback or keep that copy, then try again.");
      } else {
        toast.error(err.message || "Could not merge");
      }
    } finally {
      setBusy(null);
    }
  }

  async function ignoreGroup(g: ReviewGroup) {
    setBusy(g.id);
    try {
      await api.post(`/api/v1/admin/duplicate-review/${g.id}/ignore`);
      toast.success("Group ignored");
      await qc.invalidateQueries({ queryKey: ["admin-duplicate-review"] });
    } catch (e) {
      toast.error((e as Error).message || "Could not ignore");
    } finally {
      setBusy(null);
    }
  }

  return (
    <div>
      <PageHeader
        title="Duplicate review"
        description="Open groups from library scan (same content hash, or same artist and title within ±3 seconds). Keep one track; merge remaps history onto the winner. A 409 means the discarded copy is playing."
      />

      {q.isLoading && <p className="text-sm text-muted">Loading groups…</p>}
      {!q.isLoading && groups.length === 0 && (
        <EmptyState icon={Copy} title="No open duplicate groups" description="Scan a library to detect same-hash or matching artist/title copies. Groups with fewer than two tracks are not listed." />
      )}

      <div className="space-y-4">
        {groups.map((g) => {
          const winner = winnerByGroup[g.id] || g.tracks[0]?.id;
          return (
            <article key={g.id} className="rounded-xl border border-border bg-surface-1 p-4">
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <Badge tone="neutral">{reasonLabel(g.reason)}</Badge>
                <span className="text-sm text-muted">{g.tracks.length} tracks</span>
              </div>
              <ul className="mb-4 space-y-2">
                {g.tracks.map((t) => (
                  <li key={t.id}>
                    <label className="flex cursor-pointer items-start gap-3 rounded-lg px-2 py-1.5 hover:bg-surface-2">
                      <input
                        type="radio"
                        name={`winner-${g.id}`}
                        className="mt-1"
                        checked={winner === t.id}
                        onChange={() => setWinnerByGroup((prev) => ({ ...prev, [g.id]: t.id }))}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="font-medium">{t.title || "Untitled"}</div>
                        <div className="text-sm text-muted">
                          {t.artist || "Unknown artist"} · {formatDuration(t.duration_ms)}
                        </div>
                      </div>
                      {winner === t.id && <Badge tone="success">Keep</Badge>}
                    </label>
                  </li>
                ))}
              </ul>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" disabled={busy === g.id} onClick={() => mergeGroup(g)}>
                  <GitMerge className="h-4 w-4" />
                  Merge into keep
                </Button>
                <Button size="sm" variant="secondary" disabled={busy === g.id} onClick={() => ignoreGroup(g)}>
                  Ignore
                </Button>
              </div>
            </article>
          );
        })}
      </div>
    </div>
  );
}
