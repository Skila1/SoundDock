import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { BarChart3 } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/misc";
import { ListeningNav } from "@/features/history/ListeningNav";
import { ListenTrackList } from "@/features/history/ListenTrackList";
import { asListenTracks, formatMinutes, type StatsResponse } from "./types";

const periods = [
  { id: "week", label: "This week" },
  { id: "month", label: "This month" },
  { id: "year", label: "This year" },
  { id: "all", label: "All time" }
];

export function StatsPage() {
  const [period, setPeriod] = useState("year");
  const [includeImport, setIncludeImport] = useState(false);
  const q = useQuery({
    queryKey: ["me-stats", period, includeImport],
    queryFn: () => api.get<StatsResponse>(`/api/v1/me/stats?period=${period}${includeImport ? "&include_import=true" : ""}`)
  });
  const d = q.data;
  const tracks = asListenTracks(d?.top_tracks);
  const maxBucket = Math.max(1, ...(d?.by_bucket || []).map((b) => b.plays));
  const imported = d?.imported?.plays || 0;

  return (
    <div>
      <PageHeader
        title="Listening stats"
        description="Recap totals use local sources (web and Discord). Imported plays stay labelled and out of the totals unless you include them."
        actions={<LayoutToggle />}
      />
      <ListeningNav />
      <div className="mb-5 flex flex-wrap items-center gap-2">
        {periods.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => setPeriod(p.id)}
            className={`rounded-full px-3 py-1 text-sm ${period === p.id ? "bg-accent text-[#04140a]" : "bg-surface-2 text-muted"}`}
          >
            {p.label}
          </button>
        ))}
        <Button size="sm" variant={includeImport ? "secondary" : "ghost"} onClick={() => setIncludeImport((v) => !v)}>
          {includeImport ? "Including imported" : "Include imported"}
        </Button>
      </div>
      {q.isLoading && (
        <div className="grid gap-3 sm:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-24" />)}
        </div>
      )}
      {d && (
        <>
          <div className="mb-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard label="Plays" value={String(d.totals.plays)} />
            <StatCard label="Minutes" value={formatMinutes(d.totals.minutes)} />
            <StatCard label="Unique tracks" value={String(d.totals.unique_tracks)} />
            <StatCard label="Skips (lifetime)" value={String(d.totals.skips ?? 0)} />
          </div>
          {imported > 0 && (
            <p className="mb-6 text-sm text-muted">
              {includeImport
                ? `Totals include ${imported} imported plays.`
                : `${imported} imported plays are labelled and excluded from recap totals.`}
            </p>
          )}
          {!d.totals.plays && (
            <EmptyState
              icon={BarChart3}
              title="No local listening in this period."
              description="Plays count after 30 seconds or 50% of the track."
            />
          )}
          {(d.by_bucket || []).length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">Trend</h2>
              <div className="flex h-32 items-end gap-1 rounded-xl border border-border bg-surface-1 p-3">
                {(d.by_bucket || []).map((b) => (
                  <div
                    key={b.bucket}
                    className="flex-1 rounded-t bg-accent/80"
                    style={{ height: `${Math.max(8, (b.plays / maxBucket) * 100)}%` }}
                    title={`${new Date(b.bucket).toLocaleDateString()} · ${b.plays} plays`}
                  />
                ))}
              </div>
            </section>
          )}
          {tracks.length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">Top tracks</h2>
              <ListenTrackList tracks={tracks} subtitle={(t) => `${t.artist || t.album || ""} · ${t.plays || t.count || 0} plays`} />
            </section>
          )}
          {(d.top_artists || []).length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">Top artists</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
                {d.top_artists.map((a) => (
                  <MediaCard
                    key={a.id}
                    className="max-w-none min-w-0 w-full"
                    to={`/artists/${a.id}`}
                    id={a.id}
                    title={a.name}
                    subtitle={`${a.plays} plays`}
                    kind="artist"
                  />
                ))}
              </div>
            </section>
          )}
          {(d.top_albums || []).length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">Top albums</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
                {d.top_albums.map((a) => (
                  <MediaCard
                    key={a.id}
                    className="max-w-none min-w-0 w-full"
                    to={`/albums/${a.id}`}
                    id={a.id}
                    title={a.title}
                    subtitle={`${a.artist || ""} · ${a.plays} plays`}
                    kind="album"
                  />
                ))}
              </div>
            </section>
          )}
          <p className="text-sm text-muted">
            See <Link className="text-accent hover:underline" to="/wrapped">Wrapped</Link> for a yearly recap.
          </p>
        </>
      )}
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border bg-surface-1 p-4">
      <p className="text-xs uppercase tracking-wide text-subtle">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
    </div>
  );
}
