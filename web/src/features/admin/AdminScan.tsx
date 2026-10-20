import { Fragment, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronRight,
  Combine,
  EyeOff,
  Play,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { Badge, Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { cn, formatBytes, formatDuration, relativeTime } from "@/lib/utils";
import {
  ScanProgressBar,
  latestScan,
  scanActive,
  useScanRuns,
} from "@/features/library/ScanProgress";
import { Card, errorMessage, useTabParam } from "./adminUi";
import type { Library } from "@/types/api";

type Scope = "library" | "duplicates" | "metadata";
const scopes: { id: Scope; label: string; hint: string }[] = [
  {
    id: "library",
    label: "Library",
    hint: "Find new, changed and removed files.",
  },
  {
    id: "duplicates",
    label: "Duplicates",
    hint: "Find songs that look like the same song, by title.",
  },
  {
    id: "metadata",
    label: "Metadata cleanup",
    hint: "Fill empty details and cover art from MusicBrainz. Never overwrites what you typed.",
  },
];

type DupeTrack = {
  id: string;
  title: string;
  artist: string;
  album: string;
  duration_ms: number;
  codec: string;
  bitrate: number;
  size_bytes: number;
  plays: number;
  created_at: string;
};
type DupeGroup = {
  key: string;
  title: string;
  artist: string;
  score: number;
  reasons: string[];
  tracks: DupeTrack[];
  best_id: string;
};
type DupeResult = { scanned_at?: string; tracks?: number; groups: DupeGroup[] };
type MetaStatus = {
  busy?: boolean;
  track_count?: number;
  job?: {
    status: string;
    progress: number;
    finished_at?: string | null;
    result?: { updated?: number; unmatched?: number; covers?: number };
  } | null;
};

const dupesKey = ["admin-duplicates-scan"];

export function AdminScan() {
  const qc = useQueryClient();
  const libs = useQuery({
    queryKey: ["libraries"],
    queryFn: () => api.get<Library[]>("/api/v1/libraries"),
  });
  const scans = useScanRuns();
  const meta = useQuery({
    queryKey: ["admin-meta"],
    queryFn: () => api.get<MetaStatus>("/api/v1/admin/metadata"),
    refetchInterval: (q) => (q.state.data?.busy ? 1500 : 10000),
  });
  const dupes = useQuery({
    queryKey: dupesKey,
    queryFn: () => api.get<DupeResult>("/api/v1/admin/duplicates/scan"),
  });
  const [scopeParam, setScopeParam] = useTabParam(
    ["library", "duplicates", "metadata"] as const,
    "library",
  );
  const [picked, setPicked] = useState<Set<Scope>>(() => new Set([scopeParam]));
  const [target, setTarget] = useState("");
  const [running, setRunning] = useState(false);
  const list = libs.data || [];

  const toggleScope = (s: Scope) => {
    const next = new Set(picked);
    if (next.has(s)) next.delete(s);
    else next.add(s);
    setPicked(next);
    if (next.size === 1) setScopeParam([...next][0]);
  };

  const run = async () => {
    setRunning(true);
    const targets = target ? list.filter((l) => l.id === target) : list;
    try {
      if (picked.has("library")) {
        for (const l of targets)
          await api.post(`/api/v1/admin/libraries/${l.id}/scan`);
        scans.refetch();
      }
      if (picked.has("metadata")) {
        try {
          await api.post("/api/v1/admin/metadata/refresh");
        } catch (e) {
          if ((e as { status?: number }).status !== 409) throw e;
          toast.message("Metadata cleanup is already running");
        }
        qc.invalidateQueries({ queryKey: ["admin-meta"] });
      }
      if (picked.has("duplicates")) {
        const res = await api.post<DupeResult>(
          "/api/v1/admin/duplicates/scan",
          target ? { library_id: target } : {},
        );
        qc.setQueryData(dupesKey, res);
        toast.success(
          res.groups.length
            ? `Found ${res.groups.length} possible duplicate${res.groups.length === 1 ? "" : "s"}`
            : "No duplicates found",
        );
      }
      if (picked.has("library") || picked.has("metadata"))
        toast.success("Scan started");
    } catch (e) {
      toast.error(errorMessage(e, "Could not start the scan"));
    } finally {
      setRunning(false);
    }
  };

  const job = meta.data?.job;
  return (
    <div className="space-y-6">
      <PageHeader
        title="Scan"
        description="Check your libraries for new files, duplicates and missing details. Pick what to scan, then run it."
      />
      <Card>
        <div className="flex flex-wrap items-end gap-3">
          <label className="min-w-48 flex-1 text-sm sm:flex-none">
            <span className="mb-1 block text-xs font-medium text-muted">
              Where
            </span>
            <Select
              value={target || "all"}
              onValueChange={(v) => setTarget(v === "all" ? "" : v)}
              options={[
                { value: "all", label: "All libraries" },
                ...list.map((l) => ({ value: l.id, label: l.name })),
              ]}
            />
          </label>
          <div className="flex flex-wrap gap-2">
            {scopes.map((s) => (
              <button
                key={s.id}
                type="button"
                title={s.hint}
                aria-pressed={picked.has(s.id)}
                onClick={() => toggleScope(s.id)}
                className={cn(
                  "rounded-full border px-3 py-1.5 text-sm font-medium transition-colors",
                  picked.has(s.id)
                    ? "border-accent bg-accent/15 text-foreground"
                    : "border-border text-muted hover:bg-surface-2",
                )}
              >
                {s.label}
              </button>
            ))}
          </div>
          <Button
            className="ml-auto"
            disabled={!picked.size || running}
            onClick={run}
          >
            <Play className="h-4 w-4" /> {running ? "Starting…" : "Run scan"}
          </Button>
        </div>
        <p className="mt-2 text-xs text-subtle">
          {scopes
            .filter((s) => picked.has(s.id))
            .map((s) => s.hint)
            .join(" ") || "Pick at least one thing to scan."}
        </p>

        <div className="mt-4 space-y-3 border-t border-border pt-4 text-sm">
          {list.map((l) => {
            const s = latestScan(scans.data, l.id);
            return (
              <div key={l.id}>
                <div className="flex justify-between gap-3">
                  <span className="font-medium">{l.name}</span>
                  <span className="text-xs text-muted">
                    {scanActive(s)
                      ? "Scanning…"
                      : s?.finished_at
                        ? `Last scanned ${relativeTime(s.finished_at)}`
                        : "Not scanned yet"}
                  </span>
                </div>
                <ScanProgressBar scan={s} />
              </div>
            );
          })}
          <div>
            <div className="flex justify-between gap-3">
              <span className="font-medium">Metadata cleanup</span>
              <span className="text-xs text-muted">
                {meta.data?.busy
                  ? `Running · ${job?.progress ?? 0}%`
                  : job?.finished_at
                    ? `Last run ${relativeTime(job.finished_at)} · ${job.result?.updated ?? 0} updated, ${job.result?.unmatched ?? 0} not found`
                    : "Not run yet"}
              </span>
            </div>
            {meta.data?.busy && (
              <div className="mt-1">
                <Progress value={job?.progress ?? 0} />
              </div>
            )}
          </div>
          <div className="flex justify-between gap-3">
            <span className="font-medium">Duplicates</span>
            <span className="text-xs text-muted">
              {dupes.data?.scanned_at
                ? `Last scanned ${relativeTime(dupes.data.scanned_at)} · ${dupes.data.tracks ?? 0} songs checked`
                : "Not scanned yet"}
            </span>
          </div>
        </div>
      </Card>
      <DuplicateTable result={dupes.data} />
    </div>
  );
}

function quality(t: DupeTrack) {
  return [
    t.codec?.toUpperCase(),
    t.bitrate ? `${Math.round(t.bitrate / 1000)} kbps` : "",
    t.size_bytes ? formatBytes(t.size_bytes) : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

function scoreTone(n: number) {
  if (n >= 8) return "danger" as const;
  if (n >= 5) return "warning" as const;
  return "neutral" as const;
}

function DuplicateTable({ result }: { result?: DupeResult }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [keep, setKeep] = useState<Record<string, string>>({});
  const [minScore, setMinScore] = useState(0);
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<"score" | "title" | "copies">("score");
  const [confirmDelete, setConfirmDelete] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);

  const groups = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const g = (result?.groups || []).filter(
      (x) =>
        x.score >= minScore &&
        (!needle || `${x.title} ${x.artist}`.toLowerCase().includes(needle)),
    );
    return [...g].sort((a, b) =>
      sort === "title"
        ? a.title.localeCompare(b.title)
        : sort === "copies"
          ? b.tracks.length - a.tracks.length
          : b.score - a.score,
    );
  }, [result, minScore, q, sort]);

  if (!result?.groups?.length) {
    return result?.scanned_at ? (
      <Card title="Duplicates">
        <p className="text-sm text-muted">
          No duplicates found in the last scan.
        </p>
      </Card>
    ) : null;
  }

  const drop = (keys: string[]) => {
    qc.setQueryData<DupeResult>(dupesKey, (old) =>
      old
        ? { ...old, groups: old.groups.filter((g) => !keys.includes(g.key)) }
        : old,
    );
    setPicked((p) => {
      const n = new Set(p);
      keys.forEach((k) => n.delete(k));
      return n;
    });
  };
  const keeperOf = (g: DupeGroup) => keep[g.key] || g.best_id;
  const refreshCatalog = () =>
    qc.invalidateQueries({
      predicate: (x) =>
        ["tracks", "albums", "home", "library"].includes(String(x.queryKey[0])),
    });

  const merge = async (gs: DupeGroup[]) => {
    setBusy(true);
    try {
      for (const g of gs) {
        const winner = keeperOf(g);
        await api.post("/api/v1/admin/duplicates/merge", {
          winner_id: winner,
          loser_ids: g.tracks.map((t) => t.id).filter((id) => id !== winner),
        });
      }
      drop(gs.map((g) => g.key));
      refreshCatalog();
      toast.success(gs.length === 1 ? "Merged" : `Merged ${gs.length} groups`);
    } catch (e) {
      toast.error(errorMessage(e, "Could not merge"));
    } finally {
      setBusy(false);
    }
  };
  const ignore = async (gs: DupeGroup[]) => {
    setBusy(true);
    try {
      for (const g of gs)
        await api.post("/api/v1/admin/duplicates/ignore", {
          track_ids: g.tracks.map((t) => t.id),
        });
      drop(gs.map((g) => g.key));
      toast.success("Marked as not duplicates");
    } catch (e) {
      toast.error(errorMessage(e, "Could not save"));
    } finally {
      setBusy(false);
    }
  };
  const remove = async (ids: string[]) => {
    setBusy(true);
    try {
      await api.post("/api/v1/tracks/bulk", {
        ids,
        delete: true,
        delete_files: true,
      });
      qc.setQueryData<DupeResult>(dupesKey, (old) =>
        old
          ? {
              ...old,
              groups: old.groups
                .map((g) => ({
                  ...g,
                  tracks: g.tracks.filter((t) => !ids.includes(t.id)),
                }))
                .filter((g) => g.tracks.length > 1),
            }
          : old,
      );
      refreshCatalog();
      toast.success(`Deleted ${ids.length} song${ids.length === 1 ? "" : "s"}`);
    } catch (e) {
      toast.error(errorMessage(e, "Could not delete"));
    } finally {
      setBusy(false);
      setConfirmDelete(null);
    }
  };
  const rename = async (t: DupeTrack, title: string) => {
    if (!title.trim() || title === t.title) return;
    try {
      const m = await api.get<Record<string, unknown>>(
        `/api/v1/tracks/${t.id}/metadata`,
      );
      await api.patch(`/api/v1/tracks/${t.id}/metadata`, {
        ...m,
        title: title.trim(),
      });
      qc.setQueryData<DupeResult>(dupesKey, (old) =>
        old
          ? {
              ...old,
              groups: old.groups.map((g) => ({
                ...g,
                tracks: g.tracks.map((x) =>
                  x.id === t.id ? { ...x, title: title.trim() } : x,
                ),
              })),
            }
          : old,
      );
      toast.success("Title saved");
    } catch (e) {
      toast.error(errorMessage(e, "Could not save title"));
    }
  };

  const pickedGroups = groups.filter((g) => picked.has(g.key));
  const allPicked = groups.length > 0 && groups.every((g) => picked.has(g.key));
  const toggle = (
    set: Set<string>,
    k: string,
    fn: (s: Set<string>) => void,
  ) => {
    const n = new Set(set);
    if (n.has(k)) n.delete(k);
    else n.add(k);
    fn(n);
  };

  return (
    <Card
      title={`Duplicates (${groups.length})`}
      description="Each row is one song found more than once. 10 means almost certainly the same song. Open a row to choose which copy to keep."
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          className="max-w-xs"
          placeholder="Filter"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <div className="w-36">
          <Select
            value={String(minScore)}
            onValueChange={(v) => setMinScore(Number(v))}
            options={[
              { value: "0", label: "Any score" },
              { value: "5", label: "5 and up" },
              { value: "8", label: "8 and up" },
            ]}
          />
        </div>
        <div className="w-40">
          <Select
            value={sort}
            onValueChange={(v) => setSort(v as typeof sort)}
            options={[
              { value: "score", label: "Highest score" },
              { value: "copies", label: "Most copies" },
              { value: "title", label: "Title" },
            ]}
          />
        </div>
      </div>
      {picked.size > 0 && (
        <div className="sticky top-12 z-10 mb-2 flex flex-wrap items-center gap-2 rounded-xl border border-accent/30 bg-surface-1/95 px-3 py-2 text-sm shadow-card backdrop-blur">
          <span className="text-muted">{pickedGroups.length} selected</span>
          <Button
            size="sm"
            variant="secondary"
            disabled={busy}
            onClick={() => merge(pickedGroups)}
          >
            <Combine className="h-4 w-4" /> Merge, keep best
          </Button>
          <Button
            size="sm"
            variant="secondary"
            disabled={busy}
            onClick={() => ignore(pickedGroups)}
          >
            <EyeOff className="h-4 w-4" /> Not duplicates
          </Button>
          <Button
            size="sm"
            variant="destructive"
            disabled={busy}
            onClick={() =>
              setConfirmDelete(
                pickedGroups.flatMap((g) => g.tracks.map((t) => t.id)),
              )
            }
          >
            <Trash2 className="h-4 w-4" /> Delete all copies
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setPicked(new Set())}
          >
            Clear
          </Button>
        </div>
      )}
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full min-w-[640px] text-sm">
          <thead className="bg-surface-2/60 text-left text-xs uppercase tracking-wider text-subtle">
            <tr>
              <th className="w-10 px-3 py-2">
                <input
                  type="checkbox"
                  className="h-4 w-4 accent-accent"
                  checked={allPicked}
                  aria-label="Select all"
                  onChange={() =>
                    setPicked(
                      allPicked ? new Set() : new Set(groups.map((g) => g.key)),
                    )
                  }
                />
              </th>
              <th className="px-2 py-2">Song</th>
              <th className="px-2 py-2">Copies</th>
              <th className="px-2 py-2">Score</th>
              <th className="px-2 py-2">Why</th>
              <th className="px-2 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {groups.map((g) => {
              const isOpen = open.has(g.key);
              return (
                <Fragment key={g.key}>
                  <tr
                    className={cn(
                      "cursor-pointer border-t border-border hover:bg-surface-2/60",
                      picked.has(g.key) && "bg-accent/10",
                    )}
                    onClick={(e) => {
                      if ((e.target as HTMLElement).closest("button,input,a"))
                        return;
                      if (picked.size) toggle(picked, g.key, setPicked);
                      else toggle(open, g.key, setOpen);
                    }}
                  >
                    <td className="px-3 py-2">
                      <input
                        type="checkbox"
                        className="h-4 w-4 accent-accent"
                        checked={picked.has(g.key)}
                        aria-label={`Select ${g.title}`}
                        onChange={() => toggle(picked, g.key, setPicked)}
                      />
                    </td>
                    <td className="px-2 py-2">
                      <button
                        type="button"
                        className="flex items-center gap-1 text-left"
                        onClick={() => toggle(open, g.key, setOpen)}
                      >
                        {isOpen ? (
                          <ChevronDown className="h-4 w-4 shrink-0" />
                        ) : (
                          <ChevronRight className="h-4 w-4 shrink-0" />
                        )}
                        <span className="min-w-0">
                          <span className="block truncate font-medium">
                            {g.title}
                          </span>
                          {g.artist && (
                            <span className="block truncate text-xs capitalize text-muted">
                              {g.artist}
                            </span>
                          )}
                        </span>
                      </button>
                    </td>
                    <td className="px-2 py-2 tabular-nums">
                      {g.tracks.length}
                    </td>
                    <td className="px-2 py-2">
                      <Badge tone={scoreTone(g.score)}>{g.score}/10</Badge>
                    </td>
                    <td className="px-2 py-2 text-xs text-muted">
                      {g.reasons.join(", ")}
                    </td>
                    <td className="px-2 py-2">
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="secondary"
                          disabled={busy}
                          onClick={() => merge([g])}
                        >
                          Merge
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={busy}
                          onClick={() => ignore([g])}
                        >
                          Not duplicates
                        </Button>
                      </div>
                    </td>
                  </tr>
                  {isOpen &&
                    g.tracks.map((t) => (
                      <tr key={t.id} className="bg-surface-2/30 text-xs">
                        <td className="px-3 py-2">
                          <input
                            type="radio"
                            name={`keep-${g.key}`}
                            className="accent-accent"
                            checked={keeperOf(g) === t.id}
                            onChange={() => setKeep({ ...keep, [g.key]: t.id })}
                            aria-label="Keep this copy"
                          />
                        </td>
                        <td className="px-2 py-2" colSpan={2}>
                          <Input
                            defaultValue={t.title}
                            className="h-8 text-sm"
                            aria-label="Title"
                            onBlur={(e) => rename(t, e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter")
                                (e.target as HTMLInputElement).blur();
                            }}
                          />
                          <div className="mt-1 text-muted">
                            {[t.artist, t.album].filter(Boolean).join(" · ") ||
                              "No artist or album"}
                          </div>
                        </td>
                        <td className="px-2 py-2 text-muted">
                          {t.duration_ms ? formatDuration(t.duration_ms) : "-"}
                        </td>
                        <td className="px-2 py-2 text-muted">
                          {quality(t)}
                          <div>
                            {t.plays} play{t.plays === 1 ? "" : "s"}
                            {keeperOf(g) === t.id ? " · keeping" : ""}
                          </div>
                        </td>
                        <td className="px-2 py-2 text-right">
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={busy}
                            onClick={() => setConfirmDelete([t.id])}
                            aria-label="Delete this copy"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </td>
                      </tr>
                    ))}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
      <ConfirmDialog
        open={!!confirmDelete}
        onOpenChange={(v) => {
          if (!v) setConfirmDelete(null);
        }}
        title={`Delete ${confirmDelete?.length ?? 0} song${confirmDelete?.length === 1 ? "" : "s"}?`}
        description="They are removed from SoundDock for everyone, including playlists. Files SoundDock downloaded are deleted too; files in your own folders are not."
        confirmLabel="Delete"
        onConfirm={() => confirmDelete && remove(confirmDelete)}
      />
    </Card>
  );
}
