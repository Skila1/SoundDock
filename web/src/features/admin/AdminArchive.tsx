import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, ArchiveRestore, Loader2, Play } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { relativeTime } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/misc";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { LocalSearch, useDebounced } from "@/components/media/LibraryToolbar";
import { Card, SaveBar, SettingSwitch, StatCard, errorMessage } from "./adminUi";

type Policy = { enabled: boolean; days: number; last_run_at?: string | null; last_archived?: number };
type ArchivedTrack = { id: string; title: string; album?: string; artist?: string; archived_at: string; last_played_at?: string | null };
type Status = { policy: Policy; archived: number; pending: number; items: ArchivedTrack[] };

const PAGE = 50;

/**
 * Songs nobody has played for a while are archived: hidden from listings but
 * never deleted. Searching for and playing one brings it straight back.
 */
export function AdminArchive() {
  const qc = useQueryClient();
  const [term, setTerm] = useState("");
  const q = useDebounced(term.trim(), 300);
  const [offset, setOffset] = useState(0);
  const status = useQuery({
    queryKey: ["admin-archive", q, offset],
    queryFn: () => api.get<Status>(`/api/v1/admin/archive?limit=${PAGE}&offset=${offset}&q=${encodeURIComponent(q)}`),
    placeholderData: (prev) => prev
  });
  const [draft, setDraft] = useState<Policy | null>(null);
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [restoreAll, setRestoreAll] = useState(false);

  useEffect(() => {
    if (status.data && !draft) setDraft(status.data.policy);
  }, [status.data, draft]);
  useEffect(() => setOffset(0), [q]);

  const policy = status.data?.policy;
  const dirty = !!draft && !!policy && (draft.enabled !== policy.enabled || draft.days !== policy.days);
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["admin-archive"] });
    setSelected(new Set());
  };

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const p = await api.put<Policy>("/api/v1/admin/archive", { enabled: draft.enabled, days: Number(draft.days) || 14 });
      setDraft(p);
      toast.success("Archive settings saved");
      refresh();
    } catch (e) {
      toast.error(errorMessage(e, "Could not save archive settings"));
    } finally {
      setSaving(false);
    }
  };

  const runNow = async () => {
    setRunning(true);
    try {
      await api.post("/api/v1/admin/archive/run");
      toast.success("Archive run started");
      window.setTimeout(refresh, 2500);
    } catch (e) {
      toast.error(errorMessage(e, "Could not start the archive run"));
    } finally {
      setRunning(false);
    }
  };

  const restore = async (body: { ids?: string[]; all?: boolean }) => {
    try {
      const r = await api.post<{ restored: number }>("/api/v1/admin/archive/restore", body);
      toast.success(r.restored === 1 ? "Restored 1 song" : `Restored ${r.restored} songs`);
      refresh();
    } catch (e) {
      toast.error(errorMessage(e, "Could not restore songs"));
    }
  };

  if (status.isError) return <QueryError message={status.error instanceof Error ? status.error.message : undefined} onRetry={() => status.refetch()} />;
  const items = status.data?.items || [];
  const allOnPage = items.length > 0 && items.every((t) => selected.has(t.id));

  return (
    <div className="space-y-6">
      <PageHeader
        title="Archive"
        description="Songs nobody has played for a while are hidden from listings, playlists and libraries. Nothing is deleted: searching for and playing an archived song brings it back everywhere it was, without downloading it again."
        actions={
          <Button variant="secondary" onClick={() => void runNow()} disabled={running}>
            {running ? <Loader2 className="animate-spin" /> : <Play />} Run now
          </Button>
        }
      />
      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard label="Archived" value={status.data ? status.data.archived.toLocaleString() : "-"} icon={Archive} />
        <StatCard label="Would archive now" value={status.data ? status.data.pending.toLocaleString() : "-"} hint={policy ? `Not played in ${policy.days} days` : undefined} />
        <StatCard
          label="Last run"
          value={policy?.last_run_at ? relativeTime(policy.last_run_at) : "Never"}
          hint={policy?.last_run_at ? `${policy.last_archived ?? 0} archived` : "Runs once a day when enabled"}
        />
      </div>
      <Card title="Settings" description="Favourites, Keep forever songs and anything in a queue are never archived.">
        {draft ? (
          <div className="space-y-2">
            <SettingSwitch label="Archive automatically" description="Checks once a day." checked={draft.enabled} onChange={(v) => setDraft({ ...draft, enabled: v })} />
            <div className="flex items-center justify-between gap-4 py-2">
              <div>
                <div className="text-sm font-medium">Archive after</div>
                <p className="text-xs text-subtle">Days without a play by anyone.</p>
              </div>
              <div className="flex items-center gap-2">
                <Input className="w-24" inputMode="numeric" value={String(draft.days)} onChange={(e) => setDraft({ ...draft, days: Number(e.target.value.replace(/\D/g, "")) || 0 })} />
                <span className="text-sm text-muted">days</span>
              </div>
            </div>
          </div>
        ) : (
          <Skeleton className="h-24 w-full" />
        )}
      </Card>
      <Card
        title="Archived songs"
        actions={
          <>
            {selected.size > 0 && (
              <Button size="sm" onClick={() => void restore({ ids: [...selected] })}>
                <ArchiveRestore /> Restore {selected.size}
              </Button>
            )}
            {(status.data?.archived || 0) > 0 && (
              <Button size="sm" variant="ghost" onClick={() => setRestoreAll(true)}>
                Restore all
              </Button>
            )}
          </>
        }
      >
        <LocalSearch value={term} onChange={setTerm} placeholder="Search archived songs" className="mb-3 sm:max-w-sm" />
        {status.isLoading ? (
          <div className="space-y-2">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-11 w-full" />)}</div>
        ) : !items.length ? (
          <EmptyState icon={Archive} title={q ? "No archived songs match." : "Nothing is archived."} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-[11px] font-semibold uppercase tracking-wider text-subtle">
                <tr className="border-b border-border">
                  <th className="w-8 py-2">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-accent"
                      checked={allOnPage}
                      onChange={() => setSelected(allOnPage ? new Set() : new Set(items.map((t) => t.id)))}
                      aria-label="Select all on this page"
                    />
                  </th>
                  <th className="py-2">Song</th>
                  <th className="hidden py-2 md:table-cell">Album</th>
                  <th className="py-2">Last played</th>
                  <th className="py-2">Archived</th>
                  <th className="py-2" />
                </tr>
              </thead>
              <tbody>
                {items.map((t) => (
                  <tr key={t.id} className="border-b border-border/50 last:border-0 hover:bg-surface-2/50">
                    <td className="py-2">
                      <input
                        type="checkbox"
                        className="h-4 w-4 accent-accent"
                        checked={selected.has(t.id)}
                        onChange={() =>
                          setSelected((prev) => {
                            const next = new Set(prev);
                            if (next.has(t.id)) next.delete(t.id);
                            else next.add(t.id);
                            return next;
                          })
                        }
                        aria-label={`Select ${t.title}`}
                      />
                    </td>
                    <td className="max-w-[16rem] py-2">
                      <div className="truncate font-medium">{t.title}</div>
                      <div className="truncate text-xs text-muted">{t.artist}</div>
                    </td>
                    <td className="hidden max-w-[14rem] truncate py-2 text-muted md:table-cell">{t.album}</td>
                    <td className="py-2 text-muted">{t.last_played_at ? relativeTime(t.last_played_at) : "Never"}</td>
                    <td className="py-2 text-muted">{relativeTime(t.archived_at)}</td>
                    <td className="py-2 text-right">
                      <Button size="sm" variant="ghost" onClick={() => void restore({ ids: [t.id] })}>
                        Restore
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {(offset > 0 || items.length === PAGE) && (
          <div className="mt-3 flex justify-end gap-2">
            <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>Previous</Button>
            <Button size="sm" variant="ghost" disabled={items.length < PAGE} onClick={() => setOffset(offset + PAGE)}>Next</Button>
          </div>
        )}
      </Card>
      <SaveBar dirty={dirty} saving={saving} onSave={() => void save()} onReset={() => setDraft(policy || null)} />
      <ConfirmDialog
        open={restoreAll}
        onOpenChange={setRestoreAll}
        title="Restore every archived song?"
        description="They return to listings, playlists and personal libraries straight away. If automatic archiving is on, unplayed songs will be archived again at the next run."
        confirmLabel="Restore all"
        onConfirm={() => void restore({ all: true })}
      />
    </div>
  );
}
