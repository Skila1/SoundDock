import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CopyPlus, ListMusic } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { useTrackedJobs } from "@/components/ui/job-progress";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import type { ProviderPlaylist } from "./types";
import { createPlaylistBasedOn } from "./createBasedOn";

/** A connected service's own playlists, each with Create Playlist Based On and Import. */
export function ProviderPlaylistList({ provider, label }: { provider: string; label: string }) {
  const qc = useQueryClient();
  const trackJob = useTrackedJobs((s) => s.track);
  const [busyId, setBusyId] = useState("");
  const remote = useQuery({
    queryKey: ["provider-playlists", provider],
    queryFn: () => api.get<ProviderPlaylist[]>(`/api/v1/providers/${provider}/playlists`)
  });
  const rows = remote.data || [];
  if (!rows.length) return null;

  const run = async (pl: ProviderPlaylist, copy: boolean) => {
    setBusyId(pl.id);
    try {
      if (copy) {
        const jid = await createPlaylistBasedOn(provider, pl);
        if (jid) trackJob(jid, `Creating playlist based on ${pl.name}`);
        toast.success("Creating your playlist. It appears under SoundDock; missing songs download in the background.");
      } else {
        const r = await api.post<{ job_id?: string }>(`/api/v1/providers/${provider}/playlists/${encodeURIComponent(pl.id)}/import`, { mode: "once", name: pl.name });
        if (r?.job_id) trackJob(r.job_id, `Importing ${pl.name}`);
        toast.success("Import started. Matching your library first.");
      }
      qc.invalidateQueries({ queryKey: ["playlists"] });
    } catch (err: any) {
      toast.error(err?.message || (copy ? "Could not create the playlist" : "Import failed"));
    } finally {
      setBusyId("");
    }
  };

  return (
    <section className="mb-8">
      <h2 className="mb-3 text-sm font-medium uppercase tracking-wide text-subtle">Your {label} playlists</h2>
      <ul className="divide-y divide-border overflow-hidden rounded-2xl border border-border bg-surface-1">
        {rows.map((pl) => (
          <li key={pl.id} className="flex items-center justify-between gap-3 px-4 py-3 transition-colors hover:bg-surface-2/60">
            <Link
              to={pl.readable === false ? "#" : `/playlists/remote/${provider}/${encodeURIComponent(pl.id)}`}
              onClick={(e) => {
                if (pl.readable === false) {
                  e.preventDefault();
                  toast.message("Spotify only shares songs from playlists you own or collaborate on. Copy this one into your own Spotify playlist to open it here.");
                }
              }}
              className={`group flex min-w-0 flex-1 items-center gap-3 ${pl.readable === false ? "opacity-60" : ""}`}
            >
              {pl.artwork ? (
                <img src={pl.artwork} alt="" loading="lazy" className="h-11 w-11 shrink-0 rounded-md object-cover" />
              ) : (
                <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md bg-surface-2"><ListMusic className="h-5 w-5 text-subtle" /></div>
              )}
              <div className="min-w-0">
                <div className="truncate font-medium group-hover:underline">{pl.name}</div>
                <div className="flex items-center gap-1.5 text-xs text-muted">
                  {pl.track_count ?? 0} tracks{pl.owner ? ` · ${pl.owner}` : ""}
                  {pl.readable === false && <Badge>Followed · can&apos;t open</Badge>}
                </div>
              </div>
            </Link>
            <div className="flex shrink-0 items-center gap-2">
              <Button
                size="sm"
                disabled={busyId === pl.id || pl.readable === false}
                title="Copy the name, songs and cover into a new SoundDock playlist. Missing songs are downloaded."
                onClick={() => void run(pl, true)}
              >
                <CopyPlus /> <span className="hidden sm:inline">Create Playlist Based On</span><span className="sm:hidden">Copy</span>
              </Button>
              <Button size="sm" variant="secondary" disabled={busyId === pl.id || pl.readable === false} onClick={() => void run(pl, false)}>
                {busyId === pl.id ? "Queued…" : "Import"}
              </Button>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
