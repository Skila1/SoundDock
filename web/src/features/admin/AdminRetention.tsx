import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { formatBytes, relativeTime } from "@/lib/utils";
import { toast } from "sonner";

type Policy = {
  enabled: boolean;
  mode: string;
  interval_minutes: number;
  max_managed_bytes: number;
  prune_down_to_bytes: number;
  min_free_bytes: number;
  free_space_target_bytes: number;
  age_days: number;
  min_play_count_protect: number;
  recent_play_days: number;
  batch_size: number;
  dry_run: boolean;
};

type Status = {
  policy: Policy;
  managed_bytes: number;
  disk_path?: string;
  disk_total: number;
  disk_free: number;
  disk_error?: string;
  eligible_count: number;
  eligible_bytes: number;
  last_prune_at?: string | null;
  last_reclaimed_bytes?: number;
  last_deleted_count?: number;
  last_dry_run?: boolean;
  next_prune_at?: string | null;
  running?: boolean;
  pressure_storage?: boolean;
  pressure_free?: boolean;
};

type PreviewRow = {
  id: string;
  title: string;
  artist: string;
  size_bytes: number;
  play_count: number;
  last_played_at?: string | null;
  acquisition: string;
  reason: string;
};

type Retention = {
  log_policies: { key: string; days: number; label: string }[];
  media: Policy;
  status?: Status;
  libraries: { id: string; name: string; storage_type: string; read_only: boolean; retention_opt_in: boolean; managed: boolean; track_count: number }[];
  exclusions: { id: string; kind: string; target_id: string; created_at: string }[];
  events: {
    id: string;
    track_id?: string;
    title: string;
    artist: string;
    size_bytes: number;
    reason: string;
    last_played_at?: string | null;
    play_count: number;
    acquisition: string;
    dry_run: boolean;
    created_at: string;
  }[];
};

const modes = [
  { value: "disabled", label: "Disabled — never automatically delete acquired music" },
  { value: "age", label: "Age based — prune idle ScapeX tracks after the age threshold" },
  { value: "storage", label: "Storage limit — keep managed media below the high-water mark" },
  { value: "free_space", label: "Free-space protection — prune when the disk is too full" },
  { value: "hybrid", label: "Hybrid — age rules, then prune harder under storage pressure" }
];

function toGB(bytes?: number | null) {
  if (!bytes) return "0";
  const n = bytes / 1024 / 1024 / 1024;
  return n >= 10 ? String(Math.round(n)) : String(Math.round(n * 100) / 100);
}

function fromGB(s: string) {
  const n = Number(s);
  if (!n || n < 0) return 0;
  return Math.round(n * 1024 * 1024 * 1024);
}

export function AdminRetention() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["ret"], queryFn: () => api.get<Retention>("/api/v1/admin/retention") });
  const [enabled, setEnabled] = useState(false);
  const [mode, setMode] = useState("disabled");
  const [hours, setHours] = useState("1");
  const [maxGB, setMaxGB] = useState("0");
  const [lowGB, setLowGB] = useState("0");
  const [freeGB, setFreeGB] = useState("0");
  const [freeTargetGB, setFreeTargetGB] = useState("0");
  const [ageDays, setAgeDays] = useState("14");
  const [minPlays, setMinPlays] = useState("0");
  const [recentDays, setRecentDays] = useState("7");
  const [batch, setBatch] = useState("50");
  const [dryRun, setDryRun] = useState(false);
  const [days, setDays] = useState<Record<string, string>>({});
  const [optIn, setOptIn] = useState<Record<string, boolean>>({});
  const [exKind, setExKind] = useState("track");
  const [exId, setExId] = useState("");
  const [preview, setPreview] = useState<{ rows: PreviewRow[]; bytes: number } | null>(null);

  useEffect(() => {
    const d = q.data;
    if (!d) return;
    const p = d.media || ({} as Policy);
    setEnabled(!!p.enabled);
    setMode(p.mode || "disabled");
    setHours(String(Math.max(1, Math.round((p.interval_minutes || 60) / 60))));
    setMaxGB(toGB(p.max_managed_bytes));
    setLowGB(toGB(p.prune_down_to_bytes));
    setFreeGB(toGB(p.min_free_bytes));
    setFreeTargetGB(toGB(p.free_space_target_bytes));
    setAgeDays(String(p.age_days ?? 14));
    setMinPlays(String(p.min_play_count_protect ?? 0));
    setRecentDays(String(p.recent_play_days ?? 7));
    setBatch(String(p.batch_size ?? 50));
    setDryRun(!!p.dry_run);
    const next: Record<string, string> = {};
    (d.log_policies || []).forEach((r) => { next[r.key] = String(r.days); });
    setDays(next);
    const o: Record<string, boolean> = {};
    (d.libraries || []).forEach((l) => { o[l.id] = !!l.retention_opt_in; });
    setOptIn(o);
  }, [q.data]);

  const st = q.data?.status;
  const save = async () => {
    await api.put("/api/v1/admin/retention", {
      log_policies: Object.fromEntries((q.data?.log_policies || []).map((r) => [r.key, Number(days[r.key] ?? r.days) || 0])),
      media: {
        enabled,
        mode,
        interval_minutes: Math.max(1, Number(hours) || 1) * 60,
        max_managed_bytes: fromGB(maxGB),
        prune_down_to_bytes: fromGB(lowGB),
        min_free_bytes: fromGB(freeGB),
        free_space_target_bytes: fromGB(freeTargetGB),
        age_days: Number(ageDays) || 0,
        min_play_count_protect: Number(minPlays) || 0,
        recent_play_days: Number(recentDays) || 0,
        batch_size: Number(batch) || 50,
        dry_run: dryRun
      },
      libraries: (q.data?.libraries || []).map((l) => ({ id: l.id, retention_opt_in: !!optIn[l.id] }))
    });
    toast.success("Retention saved");
    qc.invalidateQueries({ queryKey: ["ret"] });
  };

  return (
    <div>
      <PageHeader
        title="Retention"
        description="Prune ScapeX / YouTube-acquired media so temporary listening does not fill the disk. NAS, mounted, and read-only libraries are never deleted unless you opt them in."
        actions={
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onClick={async () => {
              const res = await api.post<{ preview?: PreviewRow[]; eligible_bytes?: number }>("/api/v1/admin/retention/preview");
              setPreview({ rows: res.preview || [], bytes: res.eligible_bytes || 0 });
            }}>Preview prune</Button>
            <Button onClick={async () => {
              await api.post("/api/v1/admin/retention/run");
              toast.success("Prune queued on the Maintenance pool");
              qc.invalidateQueries({ queryKey: ["ret"] });
            }}>Run prune now</Button>
          </div>
        }
      />

      <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Stat label="Managed media" value={formatBytes(st?.managed_bytes)} hint={st?.policy?.max_managed_bytes ? `Limit ${formatBytes(st.policy.max_managed_bytes)}` : "No size limit"} />
        <Stat label="Filesystem free" value={st?.disk_error ? "Unavailable" : formatBytes(st?.disk_free)} hint={st?.disk_path || st?.disk_error || ""} />
        <Stat label="Eligible now" value={String(st?.eligible_count ?? 0)} hint={formatBytes(st?.eligible_bytes)} />
        <Stat
          label="Last prune"
          value={st?.last_prune_at ? relativeTime(st.last_prune_at) : "Never"}
          hint={st?.last_prune_at ? `${st.last_dry_run ? "Dry run · " : ""}${formatBytes(st.last_reclaimed_bytes)} reclaimed` : (st?.next_prune_at ? `Next ${relativeTime(st.next_prune_at)}` : "")}
        />
      </div>
      {(st?.pressure_storage || st?.pressure_free || st?.running) && (
        <div className="mb-4 flex flex-wrap gap-2">
          {st.running && <Badge tone="accent">Prune running</Badge>}
          {st.pressure_storage && <Badge tone="warning">Over storage limit</Badge>}
          {st.pressure_free && <Badge tone="warning">Low free disk</Badge>}
        </div>
      )}

      <section className="mb-8 max-w-2xl space-y-4 rounded-xl border border-border bg-surface-1 p-4">
        <h2 className="font-semibold">Media pruning</h2>
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-medium">Enable automatic pruning</div>
            <p className="text-xs text-subtle">Runs on the Maintenance worker pool. Playback, search, and ScapeX are not blocked.</p>
          </div>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </div>
        <Field label="Retention mode">
          <Select value={mode} onValueChange={setMode} options={modes} />
        </Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Prune interval (hours)" hint="How often automatic runs are considered.">
            <Input type="number" min={1} value={hours} onChange={(e) => setHours(e.target.value)} />
          </Field>
          <Field label="Batch size" hint="Maximum deletions per run.">
            <Input type="number" min={1} max={500} value={batch} onChange={(e) => setBatch(e.target.value)} />
          </Field>
          <Field label="Age threshold (days)" hint="Idle ScapeX tracks older than this become eligible.">
            <Input type="number" min={0} value={ageDays} onChange={(e) => setAgeDays(e.target.value)} />
          </Field>
          <Field label="Recent-play protection (days)" hint="Do not prune anything played within this window. 0 disables.">
            <Input type="number" min={0} value={recentDays} onChange={(e) => setRecentDays(e.target.value)} />
          </Field>
          <Field label="Protect after this many plays" hint="0 disables. Frequently played tracks stay.">
            <Input type="number" min={0} value={minPlays} onChange={(e) => setMinPlays(e.target.value)} />
          </Field>
          <Field label="Maximum managed storage (GB)" hint="High-water mark. 0 means no size cap.">
            <Input type="number" min={0} step="0.1" value={maxGB} onChange={(e) => setMaxGB(e.target.value)} />
          </Field>
          <Field label="Prune down to (GB)" hint="Low-water mark so pruning is not one-song-at-a-time. 0 uses 90% of the maximum.">
            <Input type="number" min={0} step="0.1" value={lowGB} onChange={(e) => setLowGB(e.target.value)} />
          </Field>
          <Field label="Minimum free disk (GB)" hint="Start pruning when free space drops below this. 0 disables.">
            <Input type="number" min={0} step="0.1" value={freeGB} onChange={(e) => setFreeGB(e.target.value)} />
          </Field>
          <Field label="Free-space target (GB)" hint="Optional extra headroom after a free-space prune. 0 adds 5 GB.">
            <Input type="number" min={0} step="0.1" value={freeTargetGB} onChange={(e) => setFreeTargetGB(e.target.value)} />
          </Field>
        </div>
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-medium">Dry-run / preview mode</div>
            <p className="text-xs text-subtle">Automatic and manual runs record what they would delete without removing files.</p>
          </div>
          <Switch checked={dryRun} onCheckedChange={setDryRun} />
        </div>
      </section>

      <section className="mb-8 max-w-2xl space-y-3">
        <h2 className="font-semibold">Libraries</h2>
        <p className="text-sm text-muted">Managed libraries are eligible for ScapeX-acquired tracks by default. Opt a NAS or local library in only if you want destructive pruning there.</p>
        <ul className="space-y-2">
          {(q.data?.libraries || []).map((l) => (
            <li key={l.id} className="flex items-center justify-between gap-3 rounded-xl border border-border bg-surface-1 px-3 py-2">
              <div className="min-w-0">
                <div className="truncate font-medium">{l.name}</div>
                <div className="text-xs text-subtle">
                  {l.storage_type}{l.read_only ? " · read-only" : ""}{l.managed ? " · managed" : ""} · {l.track_count} tracks
                </div>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted">Opt in</span>
                <Switch checked={!!optIn[l.id]} onCheckedChange={(v) => setOptIn({ ...optIn, [l.id]: v })} />
              </label>
            </li>
          ))}
        </ul>
      </section>

      <section className="mb-8 max-w-2xl space-y-3">
        <h2 className="font-semibold">Exclusions</h2>
        <p className="text-sm text-muted">Never prune these tracks, albums, artists, playlists, or libraries. Favourites, Keep forever, manual playlists, Up Next, and active jobs are already protected.</p>
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            await api.post("/api/v1/admin/retention/exclusions", { kind: exKind, target_id: exId.trim() });
            setExId("");
            toast.success("Exclusion added");
            qc.invalidateQueries({ queryKey: ["ret"] });
          }}
        >
          <Field label="Kind">
            <Select value={exKind} onValueChange={setExKind} options={["track", "album", "artist", "playlist", "library"].map((k) => ({ value: k, label: k }))} />
          </Field>
          <Field label="ID">
            <Input className="w-72" value={exId} onChange={(e) => setExId(e.target.value)} placeholder="UUID" required />
          </Field>
          <Button type="submit">Exclude</Button>
        </form>
        <ul className="space-y-1 text-sm">
          {(q.data?.exclusions || []).map((e) => (
            <li key={e.id} className="flex items-center justify-between gap-2 rounded-lg border border-border px-3 py-2">
              <span><Badge>{e.kind}</Badge> <span className="font-mono text-xs">{e.target_id}</span></span>
              <Button size="sm" variant="ghost" onClick={async () => {
                await api.del(`/api/v1/admin/retention/exclusions/${e.id}`);
                qc.invalidateQueries({ queryKey: ["ret"] });
              }}>Remove</Button>
            </li>
          ))}
        </ul>
      </section>

      <section className="mb-8 max-w-2xl space-y-3">
        <h2 className="font-semibold">Recent retention activity</h2>
        <div className="overflow-x-auto rounded-xl border border-border">
          <table className="w-full text-left text-sm">
            <thead className="bg-surface-2 text-muted">
              <tr>
                <th className="p-3">When</th>
                <th className="p-3">Track</th>
                <th className="p-3">Why</th>
                <th className="p-3">Plays</th>
                <th className="p-3">Size</th>
              </tr>
            </thead>
            <tbody>
              {(q.data?.events || []).length === 0 && (
                <tr><td className="p-3 text-subtle" colSpan={5}>No prune activity yet.</td></tr>
              )}
              {(q.data?.events || []).map((e) => (
                <tr key={e.id} className="border-t border-border">
                  <td className="p-3 text-muted">{relativeTime(e.created_at)}{e.dry_run ? " · dry" : ""}</td>
                  <td className="p-3">
                    <div className="font-medium">{e.title || "Unknown"}</div>
                    <div className="text-xs text-subtle">{e.artist}{e.acquisition ? ` · ${e.acquisition}` : ""}</div>
                  </td>
                  <td className="p-3 text-muted">{e.reason}</td>
                  <td className="p-3">{e.play_count}{e.last_played_at ? ` · ${relativeTime(e.last_played_at)}` : ""}</td>
                  <td className="p-3">{formatBytes(e.size_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="mb-8 max-w-md space-y-3">
        <h2 className="font-semibold">Logs and history</h2>
        <p className="text-sm text-muted">0 days means keep forever. These only expire database rows, not music files.</p>
        {(q.data?.log_policies || []).map((r) => (
          <Field key={r.key} label={r.label || r.key}>
            <Input type="number" min={0} value={days[r.key] ?? r.days} onChange={(e) => setDays({ ...days, [r.key]: e.target.value })} />
          </Field>
        ))}
      </section>

      <Button onClick={save}>Save retention</Button>

      <Dialog open={!!preview} onOpenChange={(v) => { if (!v) setPreview(null); }}>
        <DialogContent title="Preview prune" className="max-h-[90vh] overflow-auto">
          <p className="mb-3 text-sm text-muted">
            {preview?.rows.length || 0} tracks · {formatBytes(preview?.bytes)} would be reclaimed. Protected favourites, Keep forever, manual playlists, queue, and active jobs are excluded.
          </p>
          <ul className="space-y-2 text-sm">
            {(preview?.rows || []).map((row) => (
              <li key={row.id} className="rounded-lg border border-border px-3 py-2">
                <div className="font-medium">{row.title}</div>
                <div className="text-xs text-subtle">
                  {row.artist} · {row.reason} · {row.play_count} plays · {formatBytes(row.size_bytes)}
                  {row.last_played_at ? ` · last ${relativeTime(row.last_played_at)}` : " · never played"}
                </div>
              </li>
            ))}
            {(preview?.rows || []).length === 0 && <li className="text-subtle">Nothing is eligible to prune right now.</li>}
          </ul>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-xl border border-border bg-surface-1 p-4">
      <div className="text-xs uppercase tracking-wide text-subtle">{label}</div>
      <div className="mt-1 text-lg font-semibold">{value}</div>
      {hint && <div className="mt-1 truncate text-xs text-muted">{hint}</div>}
    </div>
  );
}
