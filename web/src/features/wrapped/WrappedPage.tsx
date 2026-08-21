import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Sparkles } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/misc";
import { relativeTime } from "@/lib/utils";
import { ListeningNav } from "@/features/history/ListeningNav";
import { ListenTrackList } from "@/features/history/ListenTrackList";
import { asListenTracks, formatMinutes, type WrappedResponse } from "@/features/stats/types";

const months = [
  { value: "0", label: "Full year" },
  { value: "1", label: "January" },
  { value: "2", label: "February" },
  { value: "3", label: "March" },
  { value: "4", label: "April" },
  { value: "5", label: "May" },
  { value: "6", label: "June" },
  { value: "7", label: "July" },
  { value: "8", label: "August" },
  { value: "9", label: "September" },
  { value: "10", label: "October" },
  { value: "11", label: "November" },
  { value: "12", label: "December" }
];

export function WrappedPage() {
  const years = useMemo(() => {
    const y = new Date().getUTCFullYear();
    return [y, y - 1, y - 2].filter((n) => n >= 2024).map((n) => ({ value: String(n), label: String(n) }));
  }, []);
  const [year, setYear] = useState(() => String(new Date().getUTCFullYear()));
  const [month, setMonth] = useState("0");
  const [includeImport, setIncludeImport] = useState(false);

  const q = useQuery({
    queryKey: ["me-wrapped", year, month, includeImport],
    queryFn: () => {
      const p = new URLSearchParams({ year });
      if (month !== "0") p.set("month", month);
      if (includeImport) p.set("include_import", "true");
      return api.get<WrappedResponse>(`/api/v1/me/wrapped?${p.toString()}`);
    }
  });
  const d = q.data;
  const tracks = asListenTracks(d?.top_tracks);
  const skipped = asListenTracks(d?.most_skipped);
  const imported = d?.imported?.plays || 0;
  const maxBucket = Math.max(1, ...(d?.by_bucket || []).map((b) => b.plays));
  const heading = month === "0" ? `${year} Wrapped` : `${months.find((m) => m.value === month)?.label} ${year}`;

  return (
    <div>
      <PageHeader
        title={heading}
        description="Local listening only (web + Discord). Imported history is labelled and excluded unless you include it."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Select className="w-28" value={year} onValueChange={setYear} options={years} />
            <Select className="w-40" value={month} onValueChange={setMonth} options={months} />
            <Button size="sm" variant={includeImport ? "secondary" : "ghost"} onClick={() => setIncludeImport((v) => !v)}>
              {includeImport ? "Including imported" : "Include imported"}
            </Button>
          </div>
        }
      />
      <ListeningNav />
      {q.isLoading && (
        <div className="grid gap-3 sm:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-28" />)}
        </div>
      )}
      {d && !d.totals.plays && (
        <EmptyState
          icon={Sparkles}
          title="Not enough local listening for a recap."
          description="Keep playing in SoundDock (web or Discord). Imported scrobbles stay out of this recap unless you label them in."
        />
      )}
      {d && d.totals.plays > 0 && (
        <>
          <div className="mb-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <HeroStat label="Minutes listened" value={formatMinutes(d.totals.minutes)} />
            <HeroStat label="Plays" value={String(d.totals.plays)} />
            <HeroStat label="Unique tracks" value={String(d.totals.unique_tracks)} />
            <HeroStat label="Artists" value={String(d.totals.unique_artists ?? 0)} />
            <HeroStat label="Albums" value={String(d.totals.unique_albums ?? 0)} />
            <HeroStat label="Skips" value={String(d.totals.skips ?? 0)} />
          </div>
          {imported > 0 && (
            <p className="mb-6 text-sm text-muted">
              {includeImport
                ? `This recap includes ${imported} imported plays.`
                : `${imported} imported plays are labelled and excluded from these totals.`}
            </p>
          )}
          {d.peak_day && (
            <p className="mb-6 text-sm text-muted">
              Loudest day: {new Date(d.peak_day.day).toLocaleDateString()} · {d.peak_day.plays} plays · {formatMinutes(d.peak_day.minutes)}
            </p>
          )}
          {d.first_listen && (
            <p className="mb-8 text-sm text-muted">
              First local play: {d.first_listen.title}
              {d.first_listen.artist ? ` — ${d.first_listen.artist}` : ""}
              {d.first_listen.played_at ? ` · ${relativeTime(d.first_listen.played_at)}` : ""}
            </p>
          )}
          {(d.by_bucket || []).length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">When you listened</h2>
              <div className="flex h-36 items-end gap-1 rounded-xl border border-border bg-surface-1 p-3">
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
              <ListenTrackList tracks={tracks} subtitle={(t) => `${t.artist || ""} · ${t.plays || 0} plays`} />
            </section>
          )}
          {(d.top_artists || []).length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">Top artists</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-5">
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
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-5">
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
          {(d.top_genres || []).length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">Top genres</h2>
              <ul className="space-y-2">
                {d.top_genres.map((g) => (
                  <li key={g.genre} className="flex items-center justify-between rounded-lg bg-surface-1 px-4 py-2 text-sm">
                    <span>{g.genre}</span>
                    <span className="text-muted">{g.plays} plays</span>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {skipped.length > 0 && (
            <section className="mb-10">
              <h2 className="mb-3 text-lg font-semibold">Most skipped</h2>
              <ListenTrackList tracks={skipped} subtitle={(t) => `${t.artist || ""} · ${t.skip_count || 0} skips`} />
            </section>
          )}
        </>
      )}
    </div>
  );
}

function HeroStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-border bg-surface-1 p-5">
      <p className="text-xs uppercase tracking-widest text-subtle">{label}</p>
      <p className="mt-2 text-3xl font-semibold">{value}</p>
    </div>
  );
}
