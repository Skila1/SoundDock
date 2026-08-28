import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitCompare } from "lucide-react";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { PageHeader } from "@/components/ui/empty";
import type { ListenComparePair, ListenCompareReport } from "@/types/api";

type Preset = "last_30_days" | "all" | "custom";

function fmt(n?: number | null) {
  if (n == null || Number.isNaN(n)) return "—";
  return new Intl.NumberFormat().format(n);
}

function fmtMin(n?: number | null) {
  if (n == null || Number.isNaN(n)) return "—";
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(n)} min`;
}

function localInput(iso?: string | null) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (x: number) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function AdminListenCompare() {
  const [preset, setPreset] = useState<Preset>("last_30_days");
  const [fromLocal, setFromLocal] = useState("");
  const [toLocal, setToLocal] = useState("");

  const qs = useMemo(() => {
    const p = new URLSearchParams();
    if (preset === "all") p.set("period", "all");
    if (preset === "custom") {
      if (fromLocal) p.set("from", new Date(fromLocal).toISOString());
      if (toLocal) p.set("to", new Date(toLocal).toISOString());
    }
    const s = p.toString();
    return s ? `?${s}` : "";
  }, [preset, fromLocal, toLocal]);

  const q = useQuery({
    queryKey: ["admin-listen-compare", qs],
    queryFn: () => api.get<ListenCompareReport>(`/api/v1/admin/listen-compare${qs}`)
  });

  const d = q.data;
  const hist = d?.history;
  const ev = d?.events;
  const diffs = d?.diffs;

  return (
    <div>
      <PageHeader
        title="Listen compare"
        description="Validation report only. History and events are compared in parallel — this is not a merged listen statistic. Home, Stats, and Wrapped still read listen_history. Recap minutes from sum(duration_ms) are labeled estimated_minutes."
      />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Badge tone={d?.ready ? "success" : "warning"}>{d?.ready ? "Events ready" : "Events not ready"}</Badge>
        <Badge tone="neutral">{(d?.period.preset || "last_30_days").replace(/_/g, " ")}</Badge>
      </div>

      <div className="mb-6 flex flex-wrap gap-2">
        <Button size="sm" variant={preset === "last_30_days" ? "default" : "secondary"} onClick={() => setPreset("last_30_days")}>
          Last 30 days
        </Button>
        <Button size="sm" variant={preset === "all" ? "default" : "secondary"} onClick={() => setPreset("all")}>
          All time
        </Button>
        <Button size="sm" variant={preset === "custom" ? "default" : "secondary"} onClick={() => setPreset("custom")}>
          Custom
        </Button>
      </div>

      {preset === "custom" && (
        <div className="mb-6 grid max-w-xl gap-3 sm:grid-cols-2">
          <Field label="From">
            <Input type="datetime-local" value={fromLocal || localInput(d?.period.from)} onChange={(e) => setFromLocal(e.target.value)} />
          </Field>
          <Field label="To">
            <Input type="datetime-local" value={toLocal || localInput(d?.period.to)} onChange={(e) => setToLocal(e.target.value)} />
          </Field>
        </div>
      )}

      {q.isError && (
        <p className="mb-4 text-sm text-destructive">{q.error instanceof Error ? q.error.message : "Could not load compare report"}</p>
      )}

      {!d?.ready && (
        <article className="mb-6 rounded-xl border border-warning/40 bg-warning/10 p-4 text-sm">
          <div className="font-medium">Shadow tables not ready</div>
          <p className="mt-1 text-muted">{d?.message || "listen_events / listen_output_segments are missing (migration 0015 pending). History figures below still come from listen_history."}</p>
        </article>
      )}

      <p className="mb-4 max-w-3xl text-sm text-muted">{d?.note || d?.period.note}</p>

      <div className="mb-8 grid gap-4 md:grid-cols-2">
        <section className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="font-semibold">listen_history</h2>
            <Badge tone="accent">production readers</Badge>
          </div>
          <dl className="space-y-2 text-sm">
            <Row label="Rows (all sources)" value={fmt(hist?.row_count)} />
            <Row label="Rows excluding import" value={fmt(hist?.row_count_excluding_import)} />
            <Row label="Import rows" value={fmt(hist?.import_row_count)} />
            <Row label="Distinct users" value={fmt(hist?.distinct_users_excluding_import)} hint="excluding import" />
            <Row label="Distinct tracks" value={fmt(hist?.distinct_tracks_excluding_import)} hint="excluding import" />
            <Row label="estimated_minutes" value={fmtMin(hist?.estimated_minutes)} hint={hist?.estimated_minutes_source || "sum(duration_ms) / 60000"} />
            <Row label="estimated_minutes excluding import" value={fmtMin(hist?.estimated_minutes_excluding_import)} />
          </dl>
        </section>

        <section className="rounded-xl border border-border bg-surface-1 p-4">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="font-semibold">listen_events</h2>
            <Badge tone="neutral">shadow</Badge>
          </div>
          <dl className="space-y-2 text-sm">
            <Row label="Rows" value={fmt(ev?.row_count)} />
            <Row label="qualified_play (all)" value={fmt(ev?.qualified_play)} />
            <Row label="qualified_play live" value={fmt(ev?.qualified_play_live)} hint="legacy_backfill = false" />
            <Row label="legacy_backfill" value={fmt(ev?.legacy_backfill)} />
            <Row label="skipped / kind=skip" value={`${fmt(ev?.skipped)} / ${fmt(ev?.kind_skip)}`} />
            <Row label="Distinct users / tracks" value={`${fmt(ev?.distinct_users)} / ${fmt(ev?.distinct_tracks)}`} />
            <Row
              label="listened minutes (incomplete)"
              value={fmtMin(ev?.listened_minutes_incomplete)}
              hint={ev?.listened_ms_note || "sum(listened_ms) for non-null only; backfill is NULL"}
            />
            <Row label="NULL listened_ms rows" value={fmt(ev?.null_listened_ms_count)} />
            <Row label="Output segments" value={fmt(ev?.output_segment_count)} />
          </dl>
        </section>
      </div>

      <section className="rounded-xl border border-border bg-surface-1 p-4">
        <div className="mb-3 flex items-center gap-2">
          <GitCompare className="h-4 w-4 text-muted" />
          <h2 className="font-semibold">Diffs</h2>
          <Badge tone="warning">not a merged total</Badge>
        </div>
        {!diffs ? (
          <p className="text-sm text-muted">Diffs are omitted until listen_events is ready.</p>
        ) : (
          <>
            <p className="mb-4 text-sm text-muted">{diffs.delta_meaning}</p>
            <div className="mb-4 overflow-x-auto">
              <table className="w-full min-w-[28rem] text-left text-sm">
                <thead>
                  <tr className="border-b border-border text-muted">
                    <th className="py-2 pr-3 font-medium">Compare</th>
                    <th className="py-2 pr-3 font-medium">History</th>
                    <th className="py-2 pr-3 font-medium">Events</th>
                    <th className="py-2 font-medium">Delta (history − events)</th>
                  </tr>
                </thead>
                <tbody>
                  <PairRow label="Plays vs live qualifies" pair={diffs.history_plays_vs_qualifies_live} hint="history excluding import vs qualified_play where legacy_backfill is false" />
                  <PairRow label="Plays vs qualifies including backfill" pair={diffs.history_plays_vs_qualifies_including_backfill} hint="all history rows vs all qualified_play events" />
                </tbody>
              </table>
            </div>
            <dl className="space-y-2 text-sm">
              <Row label="History rows with no matching event" value={fmt(diffs.history_rows_with_no_matching_event)} hint={diffs.match_key} />
              <Row label="Live events with no matching history" value={fmt(diffs.live_events_with_no_matching_history)} hint="New accumulated-time qualification vs old seek-past-T history" />
              <Row label="play_counts.skip_count (lifetime)" value={fmt(diffs.play_counts_skip_count)} />
              <Row label="Skip events (period)" value={fmt(diffs.skip_events)} />
              <Row label="Unqualified skip events" value={fmt(diffs.skip_events_unqualified)} />
              <Row label="Skip delta" value={fmt(diffs.skip_delta)} hint={diffs.skip_note} />
            </dl>
            <p className="mt-4 text-xs text-subtle">{diffs.match_key_note}</p>
          </>
        )}
      </section>
    </div>
  );
}

function Row({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <dt className="text-muted">
        {label}
        {hint && <div className="text-xs text-subtle">{hint}</div>}
      </dt>
      <dd className="font-medium tabular-nums">{value}</dd>
    </div>
  );
}

function PairRow({ label, pair, hint }: { label: string; pair: ListenComparePair; hint?: string }) {
  return (
    <tr className="border-b border-border/60">
      <td className="py-2 pr-3">
        {label}
        {hint && <div className="text-xs text-subtle">{hint}</div>}
      </td>
      <td className="py-2 pr-3 tabular-nums">{fmt(pair.history)}</td>
      <td className="py-2 pr-3 tabular-nums">{fmt(pair.events)}</td>
      <td className="py-2 tabular-nums">{fmt(pair.delta)}</td>
    </tr>
  );
}
