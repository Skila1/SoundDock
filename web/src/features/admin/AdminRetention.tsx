import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, Loader2, ShieldCheck, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Badge, Skeleton } from "@/components/ui/misc";
import { PageHeader, QueryError } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { formatBytes, relativeTime } from "@/lib/utils";
import { Card, SaveBar, SettingSwitch, errorMessage } from "./adminUi";
import { Expander, InlineNumber, StatusLine, fromGB, toGB, whenLabel } from "./storage/storageUi";
import { ExclusionPicker, kindIcon, type PickKind } from "./storage/ExclusionPicker";

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

type Exclusion = { id: string; kind: string; target_id: string; created_at: string; name?: string };

type Retention = {
  log_policies: { key: string; days: number; label: string }[];
  media: Policy;
  status?: Status;
  libraries: { id: string; name: string; storage_type: string; read_only: boolean; retention_opt_in: boolean; managed: boolean; track_count: number }[];
  exclusions: Exclusion[];
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

const triggers = [
  { value: "hybrid", label: "By age, and sooner when space runs low" },
  { value: "age", label: "By age only" },
  { value: "storage", label: "Only when downloads pass the size cap" },
  { value: "free_space", label: "Only when free disk space runs low" }
];

type Form = {
  on: boolean;
  trigger: string;
  ageDays: string;
  freeGB: string;
  hours: string;
  batch: string;
  recentDays: string;
  minPlays: string;
  maxGB: string;
  lowGB: string;
  freeTargetGB: string;
  dryRun: boolean;
  optIn: Record<string, boolean>;
  logDays: Record<string, string>;
};

function formFrom(d?: Retention): Form | null {
  if (!d) return null;
  const p = d.media || ({} as Policy);
  const on = !!p.enabled && p.mode !== "disabled";
  return {
    on,
    trigger: !p.mode || p.mode === "disabled" ? "hybrid" : p.mode,
    ageDays: String(p.age_days ?? 14),
    freeGB: toGB(p.min_free_bytes),
    hours: String(Math.max(1, Math.round((p.interval_minutes || 60) / 60))),
    batch: String(p.batch_size ?? 50),
    recentDays: String(p.recent_play_days ?? 7),
    minPlays: String(p.min_play_count_protect ?? 0),
    maxGB: toGB(p.max_managed_bytes),
    lowGB: toGB(p.prune_down_to_bytes),
    freeTargetGB: toGB(p.free_space_target_bytes),
    dryRun: !!p.dry_run,
    optIn: Object.fromEntries((d.libraries || []).map((l) => [l.id, !!l.retention_opt_in])),
    logDays: Object.fromEntries((d.log_policies || []).map((r) => [r.key, String(r.days)]))
  };
}

function payloadFrom(f: Form, d: Retention) {
  return {
    log_policies: Object.fromEntries((d.log_policies || []).map((r) => [r.key, Number(f.logDays[r.key] ?? r.days) || 0])),
    media: {
      enabled: f.on,
      mode: f.on ? f.trigger : "disabled",
      interval_minutes: Math.max(1, Number(f.hours) || 1) * 60,
      max_managed_bytes: fromGB(f.maxGB),
      prune_down_to_bytes: fromGB(f.lowGB),
      min_free_bytes: fromGB(f.freeGB),
      free_space_target_bytes: fromGB(f.freeTargetGB),
      age_days: Number(f.ageDays) || 0,
      min_play_count_protect: Number(f.minPlays) || 0,
      recent_play_days: Number(f.recentDays) || 0,
      batch_size: Number(f.batch) || 50,
      dry_run: f.dryRun
    },
    libraries: (d.libraries || []).map((l) => ({ id: l.id, retention_opt_in: !!f.optIn[l.id] }))
  };
}

const retKey = ["ret"];

/** Cleanup: deletes downloaded files nobody plays so the disk does not fill up. */
export function AdminRetention() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: retKey, queryFn: () => api.get<Retention>("/api/v1/admin/retention") });
  const server = useMemo(() => formFrom(q.data), [q.data]);
  const [form, setForm] = useState<Form | null>(null);
  const [saving, setSaving] = useState(false);
  const [preview, setPreview] = useState<{ rows: PreviewRow[]; bytes: number } | null>(null);
  const [previewing, setPreviewing] = useState(false);

  useEffect(() => { setForm(server); }, [server]);

  const d = q.data;
  const st = d?.status;
  const f = form;
  const set = (patch: Partial<Form>) => setForm((cur) => (cur ? { ...cur, ...patch } : cur));
  const dirty = !!f && !!server && JSON.stringify(f) !== JSON.stringify(server);
  const savedOn = !!server?.on;

  const save = async () => {
    if (!f || !d) return;
    setSaving(true);
    try {
      await api.put("/api/v1/admin/retention", payloadFrom(f, d));
      toast.success("Cleanup saved");
      await qc.invalidateQueries({ queryKey: retKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save cleanup settings"));
    } finally {
      setSaving(false);
    }
  };

  const openPreview = async () => {
    setPreviewing(true);
    try {
      const res = await api.post<{ preview?: PreviewRow[]; eligible_bytes?: number }>("/api/v1/admin/retention/preview");
      setPreview({ rows: res.preview || [], bytes: res.eligible_bytes || 0 });
    } catch (e) {
      toast.error(errorMessage(e, "Could not preview"));
    } finally {
      setPreviewing(false);
    }
  };

  const runNow = async () => {
    try {
      await api.post("/api/v1/admin/retention/run");
      toast.success(server?.dryRun ? "Dry run started. Nothing will be deleted." : "Cleanup started");
      setPreview(null);
      void qc.invalidateQueries({ queryKey: retKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not start cleanup"));
    }
  };

  const excluded = useMemo(() => new Set((d?.exclusions || []).map((e) => `${e.kind}:${e.target_id}`)), [d?.exclusions]);
  const addExclusion = async (kind: PickKind, id: string, title: string) => {
    try {
      await api.post("/api/v1/admin/retention/exclusions", { kind, target_id: id });
      toast.success(`${title} is protected`);
      void qc.invalidateQueries({ queryKey: retKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not protect it"));
    }
  };
  const removeExclusion = async (id: string) => {
    try {
      await api.del(`/api/v1/admin/retention/exclusions/${id}`);
      void qc.invalidateQueries({ queryKey: retKey });
    } catch (e) {
      toast.error(errorMessage(e, "Could not remove"));
    }
  };

  if (q.isError) return <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />;

  const usesAge = f ? f.trigger === "age" || f.trigger === "hybrid" : true;
  const usesFree = f ? f.trigger === "free_space" || f.trigger === "hybrid" : true;
  const events = (d?.events || []).slice(0, 8);

  return (
    <div className="space-y-5">
      <PageHeader
        title="Cleanup"
        description="Deletes downloaded files nobody plays so they do not fill the disk. Your own folders and network libraries are left alone. Cleanup deletes the file; Archive only hides the song."
        actions={
          <Button variant="secondary" onClick={() => void openPreview()} disabled={previewing}>
            {previewing ? <Loader2 className="animate-spin" /> : <Eye />} Preview cleanup
          </Button>
        }
      />

      <Card>
        {!f ? (
          <Skeleton className="h-24 w-full" />
        ) : (
          <div className="space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0">
                <h2 className="text-base font-semibold tracking-tight">Automatic cleanup</h2>
                <p className="text-sm text-muted">{f.on ? "On. Runs in the background without slowing playback." : "Off. Nothing is deleted automatically."}</p>
              </div>
              <Switch checked={f.on} onCheckedChange={(on) => set({ on })} aria-label="Automatic cleanup" />
            </div>
            <p className={`text-[15px] leading-10 ${f.on ? "" : "opacity-60"}`}>
              Remove downloaded files
              {usesAge ? (
                <> not played in<InlineNumber label="Days without a play" value={f.ageDays} onChange={(ageDays) => set({ ageDays })} disabled={!f.on} />days</>
              ) : (
                <> when space runs low</>
              )}
              {usesFree && (
                <>, keep at least<InlineNumber label="Free space to keep in GB" step="0.5" value={f.freeGB} onChange={(freeGB) => set({ freeGB })} disabled={!f.on} />GB free</>
              )}
              .
            </p>
            {st && (
              <div className="rounded-xl bg-surface-2/60 px-4 py-3">
                <StatusLine
                  items={[
                    <span key="m">{formatBytes(st.managed_bytes)} downloaded</span>,
                    st.disk_error ? <span key="f">Free space unknown</span> : <span key="f">{formatBytes(st.disk_free)} free</span>,
                    <span key="e"><span className="font-medium text-foreground">{st.eligible_count}</span> files ({formatBytes(st.eligible_bytes)}) could go now</span>,
                    <span key="l">Last run {st.last_prune_at ? `${relativeTime(st.last_prune_at)}${st.last_dry_run ? " (dry run)" : `, freed ${formatBytes(st.last_reclaimed_bytes)}`}` : "never"}</span>,
                    savedOn && st.next_prune_at && <span key="n">next {whenLabel(st.next_prune_at)}</span>
                  ]}
                />
                {(st.running || st.pressure_storage || st.pressure_free) && (
                  <div className="mt-2 flex flex-wrap gap-2">
                    {st.running && <Badge tone="accent">Running now</Badge>}
                    {st.pressure_storage && <Badge tone="warning">Over the size cap</Badge>}
                    {st.pressure_free && <Badge tone="warning">Low free space</Badge>}
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </Card>

      {f && (
        <Expander title="Advanced" description="When it runs, how much at a time, extra protection, dry run and which libraries.">
          <div className="space-y-5">
            <Row label="What triggers cleanup">
              <Select className="sm:w-80" value={f.trigger} onValueChange={(trigger) => set({ trigger })} options={triggers} />
            </Row>
            <Row label="Check every" hint="Hours between automatic runs.">
              <Num value={f.hours} onChange={(hours) => set({ hours })} min={1} unit="hours" />
            </Row>
            <Row label="Files per run" hint="Most files removed in one run (up to 500).">
              <Num value={f.batch} onChange={(batch) => set({ batch })} min={1} unit="files" />
            </Row>
            <Row label="Keep anything played in the last" hint="0 turns this off.">
              <Num value={f.recentDays} onChange={(recentDays) => set({ recentDays })} unit="days" />
            </Row>
            <Row label="Keep songs played at least" hint="0 turns this off.">
              <Num value={f.minPlays} onChange={(minPlays) => set({ minPlays })} unit="times" />
            </Row>
            <Row label="Size cap for downloads" hint="Start cleaning above this. 0 means no cap.">
              <Num value={f.maxGB} onChange={(maxGB) => set({ maxGB })} step="0.5" unit="GB" />
            </Row>
            <Row label="Then clean down to" hint="0 means 90% of the cap.">
              <Num value={f.lowGB} onChange={(lowGB) => set({ lowGB })} step="0.5" unit="GB" />
            </Row>
            <Row label="When space runs low, free up to" hint="0 frees 5 GB more than the minimum.">
              <Num value={f.freeTargetGB} onChange={(freeTargetGB) => set({ freeTargetGB })} step="0.5" unit="GB" />
            </Row>
            <SettingSwitch label="Dry run" description="Record what would be deleted without deleting anything." checked={f.dryRun} onChange={(dryRun) => set({ dryRun })} />
            {(d?.libraries || []).length > 0 && (
              <div>
                <div className="text-sm font-medium">Libraries</div>
                <p className="mb-2 text-xs text-subtle">Downloads in SoundDock's own media folder are always included. Turn another library on only if SoundDock may delete files there.</p>
                <ul className="divide-y divide-border rounded-xl border border-border">
                  {(d?.libraries || []).map((l) => (
                    <li key={l.id} className="flex items-center justify-between gap-3 px-3 py-2">
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium">{l.name}</div>
                        <div className="text-xs text-subtle">{l.track_count} tracks{l.managed ? " · SoundDock media" : ` · ${l.storage_type}`}{l.read_only ? " · read only" : ""}</div>
                      </div>
                      <Switch checked={!!f.optIn[l.id]} onCheckedChange={(v) => set({ optIn: { ...f.optIn, [l.id]: v } })} aria-label={`Clean up ${l.name}`} />
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        </Expander>
      )}

      <Card title="Always keep" description="Favourites, Keep forever songs, playlists, queues and anything playing are never removed. Add anything else here.">
        <ExclusionPicker excluded={excluded} onPick={(kind, hit) => void addExclusion(kind, hit.id, hit.title)} />
        {(d?.exclusions || []).length > 0 && (
          <ul className="mt-3 flex flex-wrap gap-2">
            {(d?.exclusions || []).map((e) => {
              const Icon = kindIcon[e.kind as PickKind] || ShieldCheck;
              return (
                <li key={e.id} className="flex max-w-full items-center gap-2 rounded-full border border-border bg-surface-2 py-1 pl-3 pr-1 text-sm">
                  <Icon className="h-3.5 w-3.5 shrink-0 text-muted" />
                  <span className="min-w-0 truncate">{e.name || <span className="text-subtle">Removed {e.kind}</span>}</span>
                  <button type="button" className="rounded-full p-1 text-muted hover:bg-surface-3 hover:text-foreground" aria-label={`Stop protecting ${e.name || e.kind}`} onClick={() => void removeExclusion(e.id)}>
                    <X className="h-3.5 w-3.5" />
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </Card>

      <Card title="Recent activity">
        {!events.length ? (
          <p className="text-sm text-subtle">Nothing removed yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-[11px] font-semibold uppercase tracking-wider text-subtle">
                <tr className="border-b border-border">
                  <th className="py-2 pr-3">Song</th>
                  <th className="hidden py-2 pr-3 sm:table-cell">Why</th>
                  <th className="py-2 pr-3 text-right">Size</th>
                  <th className="py-2 text-right">When</th>
                </tr>
              </thead>
              <tbody>
                {events.map((e) => (
                  <tr key={e.id} className="border-b border-border/50 last:border-0">
                    <td className="max-w-[14rem] py-2 pr-3">
                      <div className="truncate font-medium">{e.title || "Unknown"}</div>
                      <div className="truncate text-xs text-subtle">{e.artist}</div>
                    </td>
                    <td className="hidden py-2 pr-3 text-muted sm:table-cell">{e.reason}{e.dry_run ? <Badge className="ml-2">dry run</Badge> : null}</td>
                    <td className="tabular py-2 pr-3 text-right text-muted">{formatBytes(e.size_bytes)}</td>
                    <td className="whitespace-nowrap py-2 text-right text-muted">{relativeTime(e.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {f && (d?.log_policies || []).length > 0 && (
        <Card title="Log history" description="How long old records are kept. 0 keeps them forever. This never touches music files.">
          <div className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
            {(d?.log_policies || []).map((r) => (
              <div key={r.key} className="flex items-center justify-between gap-3 py-1">
                <span className="text-sm">{r.label || r.key}</span>
                <Num value={f.logDays[r.key] ?? String(r.days)} onChange={(v) => set({ logDays: { ...f.logDays, [r.key]: v } })} unit="days" />
              </div>
            ))}
          </div>
        </Card>
      )}

      <SaveBar dirty={dirty} saving={saving} onSave={() => void save()} onReset={() => setForm(server)} />

      <Dialog open={!!preview} onOpenChange={(v) => { if (!v) setPreview(null); }}>
        <DialogContent title="Preview cleanup" className="max-h-[90vh] overflow-auto">
          <p className="mb-3 text-sm text-muted">
            {preview?.rows.length ? `${preview.rows.length} files, ${formatBytes(preview.bytes)} in total, would be removed by a run now.` : "Nothing would be removed right now."}
            {server?.dryRun ? " Dry run is on, so a run only records this list." : ""}
          </p>
          {!savedOn && <p className="mb-3 text-sm text-warning">Turn on automatic cleanup and save to run it.</p>}
          <div className="mb-3 flex gap-2">
            <Button variant={server?.dryRun ? "default" : "destructive"} disabled={!preview?.rows.length || !savedOn} onClick={() => void runNow()}>
              <Trash2 /> {server?.dryRun ? "Run dry run" : `Remove ${preview?.rows.length || 0} files`}
            </Button>
            <Button variant="ghost" onClick={() => setPreview(null)}>Cancel</Button>
          </div>
          <ul className="divide-y divide-border rounded-xl border border-border text-sm">
            {(preview?.rows || []).map((row) => (
              <li key={row.id} className="flex items-center justify-between gap-3 px-3 py-2">
                <div className="min-w-0">
                  <div className="truncate font-medium">{row.title}</div>
                  <div className="truncate text-xs text-subtle">
                    {row.artist} · {row.play_count} plays · {row.last_played_at ? `last ${relativeTime(row.last_played_at)}` : "never played"}
                  </div>
                </div>
                <span className="tabular shrink-0 text-xs text-muted">{formatBytes(row.size_bytes)}</span>
              </li>
            ))}
          </ul>
          <p className="mt-3 text-xs text-subtle">Want to keep something? Add it under Always keep. Hiding songs instead of deleting them is done by <Link to="/admin/archive" className="text-accent hover:underline">Archive</Link>.</p>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        {hint && <p className="text-xs text-subtle">{hint}</p>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

function Num({ value, onChange, unit, min = 0, step }: { value: string; onChange: (v: string) => void; unit: string; min?: number; step?: string }) {
  return (
    <div className="flex items-center gap-2">
      <Input className="h-9 w-24" type="number" inputMode="decimal" min={min} step={step} value={value} onChange={(e) => onChange(e.target.value)} />
      <span className="w-12 text-sm text-muted">{unit}</span>
    </div>
  );
}
