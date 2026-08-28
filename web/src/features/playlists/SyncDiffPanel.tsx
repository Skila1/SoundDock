import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { relativeTime } from "@/lib/utils";
import type { SyncDiff } from "./types";

function confidenceLabel(n: number | null | undefined) {
  if (n == null || Number.isNaN(Number(n))) return "-";
  return Number(n).toFixed(2);
}

export function SyncDiffPanel({ playlistId }: { playlistId: string }) {
  const q = useQuery({
    queryKey: ["sync-diff", playlistId],
    queryFn: () => api.get<SyncDiff>(`/api/v1/playlists/${playlistId}/sync-diff`)
  });
  const d = q.data;
  if (!d || (!d.items?.length && !d.provider)) return null;
  return (
    <section className="mb-8 rounded-xl border border-border bg-surface-1 p-4">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="font-semibold">Sync diff</h2>
          <p className="text-sm text-muted">
            {d.matched} matched · {d.unmatched} unmatched
            {d.ignored ? ` · ${d.ignored} ignored` : ""}
            {d.last_sync_at ? ` · ${relativeTime(d.last_sync_at)}` : ""}
          </p>
        </div>
        {d.status && <Badge tone={d.status === "ok" ? "success" : d.status === "failed" ? "danger" : "neutral"}>{d.status}</Badge>}
      </div>
      {d.error && <p className="mb-3 text-sm text-destructive">{d.error}</p>}
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="text-xs uppercase tracking-wide text-subtle">
            <tr>
              <th className="py-1 pr-3 font-medium">Title</th>
              <th className="py-1 pr-3 font-medium">Status</th>
              <th className="py-1 font-medium">match_confidence</th>
            </tr>
          </thead>
          <tbody>
            {(d.items || []).map((it) => (
              <tr key={it.id} className="border-t border-border/60">
                <td className="py-2 pr-3">
                  <div className="font-medium">{it.title}</div>
                  <div className="text-xs text-muted">{it.artists}{it.album ? ` · ${it.album}` : ""}</div>
                </td>
                <td className="py-2 pr-3">
                  <Badge tone={it.mapped_track_id ? "success" : it.match_status === "ambiguous" ? "warning" : "neutral"}>
                    {it.match_status || (it.mapped_track_id ? "matched" : "unmatched")}
                  </Badge>
                </td>
                <td className="py-2 tabular-nums text-muted">{confidenceLabel(it.match_confidence)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
