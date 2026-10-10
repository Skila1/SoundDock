import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import type { ReactNode } from "react";
import { BarChart3 } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/misc";
import { ListeningNav } from "@/features/history/ListeningNav";
import { TrackList } from "@/components/media/TrackList";
import { TrackCard } from "@/components/media/TrackActions";
import { ProgressiveGrid } from "@/components/media/LibraryToolbar";
import { Artwork } from "@/components/media/Artwork";
import { usePlayer } from "@/stores/player";
import { useLibraryView } from "@/stores/libraryView";
import { artworkUrl } from "@/lib/utils";
import { toast } from "sonner";
import { asListenTracks, formatMinutes, type StatsResponse } from "./types";

const periods = [
  { id: "week", label: "This week" },
  { id: "month", label: "This month" },
  { id: "year", label: "This year" },
  { id: "all", label: "All time" }
];

export function StatsPage() {
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const [view, patchView] = useLibraryView("stats", { layout: "list" });
  const layout = view.layout;
  const [period, setPeriod] = useState("year");
  const [includeImport, setIncludeImport] = useState(false);
  const q = useQuery({
    queryKey: ["me-stats", period, includeImport],
    queryFn: () => api.get<StatsResponse>(`/api/v1/me/stats?period=${period}${includeImport ? "&include_import=true" : ""}`)
  });
  const d = q.data;
  const tracks = asListenTracks(d?.top_tracks);
  const skipped = asListenTracks(d?.most_skipped);
  const maxBucket = Math.max(1, ...(d?.by_bucket || []).map((b) => b.plays));
  const imported = d?.imported?.plays || 0;

  return (
    <div>
      <PageHeader
        title="Listening stats"
        description="Recap totals use local sources (web and Discord). Imported plays stay labelled and out of the totals unless you include them."
        actions={<LayoutToggle value={layout} onChange={(l) => patchView({ layout: l })} />}
      />
      <ListeningNav />
      <div className="mb-6 flex flex-wrap items-center gap-2">
        <div className="inline-flex flex-wrap gap-1 rounded-full bg-surface-2/70 p-1 ring-1 ring-inset ring-border">
        {periods.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => setPeriod(p.id)}
            className={`rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors ${period === p.id ? "bg-surface-1 text-foreground shadow-sm" : "text-muted hover:text-foreground"}`}
          >
            {p.label}
          </button>
        ))}
        </div>
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
          {d.peak_day && (
            <p className="mb-8 text-sm text-muted">
              Busiest day: {new Date(d.peak_day.day).toLocaleDateString()} · {d.peak_day.plays} plays · {formatMinutes(d.peak_day.minutes)}
            </p>
          )}
          {tracks.length > 0 && (
            <StatsSection title="Top tracks">
              {layout === "list" ? (
                <TrackList
                  tracks={tracks}
                  live={false}
                  onPlay={(i) => play([tracks[i].id])}
                  onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
                  onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
                />
              ) : (
                <ProgressiveGrid items={tracks} render={(t) => <TrackCard key={t.id} track={t} subtitle={`${t.artist || t.album || ""} · ${t.plays || t.count || 0} plays`} onPlay={() => play([t.id])} />} />
              )}
            </StatsSection>
          )}
          {(d.top_artists || []).length > 0 && (
            <StatsSection title="Top artists">
              {layout === "list" ? (
                <RankedRows rows={d.top_artists.map((a) => ({ id: a.id, to: `/artists/${a.id}`, title: a.name, meta: `${a.plays} plays`, art: artworkUrl("artist", a.id, "thumb"), kind: "artist" as const }))} />
              ) : (
                <ProgressiveGrid items={d.top_artists} render={(a) => <MediaCard key={a.id} className="w-full min-w-0 max-w-none" to={`/artists/${a.id}`} id={a.id} title={a.name} subtitle={`${a.plays} plays`} kind="artist" />} />
              )}
            </StatsSection>
          )}
          {(d.top_albums || []).length > 0 && (
            <StatsSection title="Top albums">
              {layout === "list" ? (
                <RankedRows rows={d.top_albums.map((a) => ({ id: a.id, to: `/albums/${a.id}`, title: a.title, meta: `${a.artist ? `${a.artist} · ` : ""}${a.plays} plays`, art: artworkUrl("album", a.id, "thumb"), kind: "album" as const }))} />
              ) : (
                <ProgressiveGrid items={d.top_albums} render={(a) => <MediaCard key={a.id} className="w-full min-w-0 max-w-none" to={`/albums/${a.id}`} id={a.id} title={a.title} subtitle={`${a.artist || ""} · ${a.plays} plays`} kind="album" />} />
              )}
            </StatsSection>
          )}
          {(d.top_genres || []).length > 0 && (
            <StatsSection title="Top genres">
              <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
                {(d.top_genres || []).map((g, i) => (
                  <li key={g.genre} className="flex items-center justify-between rounded-xl border border-border bg-surface-1 px-4 py-3 text-sm">
                    <span className="flex min-w-0 items-center gap-3">
                      <span className="tabular w-5 text-subtle">{i + 1}</span>
                      <span className="truncate font-medium">{g.genre}</span>
                    </span>
                    <span className="tabular text-muted">{g.plays}</span>
                  </li>
                ))}
              </ul>
            </StatsSection>
          )}
          {skipped.length > 0 && (
            <StatsSection title="Most skipped">
              <TrackList
                tracks={skipped}
                live={false}
                onPlay={(i) => play([skipped[i].id])}
                onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
              />
            </StatsSection>
          )}
        </>
      )}
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-border bg-surface-1 p-5 shadow-sm">
      <p className="text-xs uppercase tracking-wide text-subtle">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
    </div>
  );
}

function StatsSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="mb-10">
      <h2 className="mb-4 text-xl font-bold tracking-tight">{title}</h2>
      {children}
    </section>
  );
}

/** Numbered rows for ranked artists and albums in list layout. */
function RankedRows({ rows }: { rows: { id: string; to: string; title: string; meta: string; art: string; kind: "artist" | "album" }[] }) {
  return (
    <ol className="divide-y divide-border/60">
      {rows.map((r, i) => (
        <li key={r.id}>
          <Link to={r.to} className="flex items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-surface-2/80">
            <span className="tabular w-6 text-center text-sm text-subtle">{i + 1}</span>
            <span className={`h-10 w-10 shrink-0 overflow-hidden ring-1 ring-inset ring-border ${r.kind === "artist" ? "rounded-full" : "rounded-md"}`}>
              <Artwork src={r.art} id={r.id} name={r.title} kind={r.kind} size="sm" />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">{r.title}</span>
              <span className="block truncate text-xs text-muted">{r.meta}</span>
            </span>
          </Link>
        </li>
      ))}
    </ol>
  );
}
