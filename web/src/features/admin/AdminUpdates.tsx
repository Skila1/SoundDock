import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CheckCircle2, Download, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge, Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { cn, relativeTime } from "@/lib/utils";
import { Card, SettingSwitch, errorMessage } from "./adminUi";

type Release = { version: string; notes: string[] };

export function AdminUpdates() {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [watch, setWatch] = useState(false);
  const q = useQuery({
    queryKey: ["admin-updates"],
    queryFn: () => api.get<any>("/api/v1/admin/updates"),
    refetchInterval: (query) => {
      const cur = query.state.data as { updating?: boolean; last_status?: string } | undefined;
      if (watch || cur?.updating || cur?.last_status === "updating") return 1000;
      return 8000;
    }
  });
  const d = q.data || {};
  const stage = d.progress?.stage;
  const updating = !!(d.updating || d.last_status === "updating" || stage === "pulling" || stage === "restarting" || stage === "queued");
  const changelog: Release[] = Array.isArray(d.changelog) ? d.changelog : [];
  const pct = typeof d.progress?.percent === "number" ? d.progress.percent : updating ? 12 : 0;
  const installed = d.version || "unknown";
  const latest = d.latest_version || d.version || "unknown";
  const available = !!d.available;
  const cannotApply = !d.can_apply;

  useEffect(() => {
    if (!watch || updating) return;
    if (d.last_status === "error" || d.last_status === "needs_recovery") {
      setWatch(false);
      toast.error(d.last_error || (d.last_status === "needs_recovery" ? "Update needs recovery" : "Update failed"));
      return;
    }
    if (d.last_status === "ok") {
      setWatch(false);
      toast.success(`Updated to ${d.version || "the latest version"}`);
      const t = setTimeout(() => location.reload(), 800);
      return () => clearTimeout(t);
    }
  }, [watch, updating, d.last_status, d.last_error, d.version]);

  async function check() {
    setBusy(true);
    try {
      await api.post("/api/v1/admin/updates/check");
      await qc.invalidateQueries({ queryKey: ["admin-updates"] });
      toast.success("Checked for updates");
    } catch (e) {
      toast.error(errorMessage(e, "Could not check for updates"));
    } finally {
      setBusy(false);
    }
  }

  async function apply() {
    setBusy(true);
    try {
      await api.post("/api/v1/admin/updates/apply");
      setWatch(true);
      toast.success("Update started");
      qc.invalidateQueries({ queryKey: ["admin-updates"] });
    } catch (e) {
      toast.error(errorMessage(e, "Could not start update"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <PageHeader
        title="Updates"
        description="Install new SoundDock releases. Your music and settings stay put during an update."
        actions={
          <Button variant="secondary" disabled={busy || d.checking || updating} onClick={check}>
            <RefreshCw className={cn(d.checking && "animate-spin")} /> Check now
          </Button>
        }
      />

      <div className="space-y-5">
        <section className={cn("rounded-2xl border bg-surface-1 p-5 shadow-sm sm:p-6", available ? "border-accent/40" : "border-border")}>
          <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
            <div>
              <div className="text-xs font-semibold uppercase tracking-wider text-subtle">Installed</div>
              <div className="tabular text-2xl font-bold tracking-tight sm:text-3xl">{installed}</div>
            </div>
            <ArrowRight className={cn("h-5 w-5 shrink-0", available ? "text-accent" : "text-subtle")} />
            <div>
              <div className="text-xs font-semibold uppercase tracking-wider text-subtle">Latest</div>
              <div className={cn("tabular text-2xl font-bold tracking-tight sm:text-3xl", available && "text-accent")}>{latest}</div>
            </div>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              {d.needs_recovery ? (
                <Badge tone="danger">Needs recovery</Badge>
              ) : updating ? (
                <Badge tone="accent">Updating</Badge>
              ) : available ? (
                <Badge tone="warning">Update available</Badge>
              ) : (
                <Badge tone="success"><CheckCircle2 className="h-3 w-3" /> Up to date</Badge>
              )}
              {d.schema_forward_only && available ? <Badge tone="warning">Cannot be rolled back</Badge> : null}
            </div>
          </div>
          <p className="mt-3 text-sm text-muted">
            Checked {d.last_check_at ? relativeTime(d.last_check_at) : "never"}
            {d.last_applied_at ? ` · last updated ${relativeTime(d.last_applied_at)}${d.last_applied_by ? ` by ${d.last_applied_by}` : ""}` : ""}
          </p>

          {d.last_error && !updating && <p className="mt-3 rounded-lg border border-destructive/40 px-3 py-2 text-sm text-destructive">{d.last_error}</p>}
          {d.needs_recovery && (
            <p className="mt-3 rounded-lg border border-destructive/40 px-3 py-2 text-sm text-destructive">
              This update changed the database and then failed, so the previous version was not restarted. Restore the backup taken before the update{d.backup_path ? ` (${d.backup_path})` : ""} from Backups.
            </p>
          )}

          {updating ? (
            <div className="mt-5 space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="font-medium">{stage === "restarting" ? "Starting the new version" : stage === "queued" ? "Waiting for the host" : "Downloading"}</span>
                <span className="tabular text-muted">{pct}%</span>
              </div>
              <Progress value={pct} />
              <p className="text-xs text-subtle">{d.progress?.detail || "Downloading the new version."}</p>
            </div>
          ) : (
            available && (
              <div className="mt-5 flex flex-wrap items-center gap-3">
                <Button disabled={busy || cannotApply || d.needs_recovery} onClick={apply}><Download /> Update now</Button>
                {cannotApply && <p className="text-sm text-muted">{d.apply_reason || "This server cannot install updates by itself. Re-run the SoundDock installer on the host to turn on one-click updates."}</p>}
              </div>
            )
          )}
        </section>

        {available && (
          <Card title="What's new">
            {changelog.length ? (
              <ul className="space-y-4">
                {changelog.map((rel) => (
                  <li key={rel.version}>
                    <div className="text-sm font-semibold">{rel.version}</div>
                    <ul className="mt-1 list-disc space-y-1 pl-5 text-sm text-muted">
                      {(rel.notes || []).map((n, i) => <li key={i}>{n}</li>)}
                    </ul>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted">Release notes appear once SoundDock can reach GitHub.</p>
            )}
          </Card>
        )}

        <section className="rounded-2xl border border-border bg-surface-1 px-5 py-3 shadow-sm">
          <SettingSwitch
            label="Automatic updates"
            description="Checks about once an hour and installs new versions by itself."
            checked={!!d.auto_enabled}
            onChange={(v) =>
              void api
                .put("/api/v1/admin/updates", { auto_enabled: v })
                .then(() => {
                  toast.success(v ? "Automatic updates on" : "Automatic updates off");
                  qc.invalidateQueries({ queryKey: ["admin-updates"] });
                })
                .catch((e) => toast.error(errorMessage(e, "Could not change automatic updates")))
            }
          />
        </section>

        <details className="rounded-2xl border border-border bg-surface-1 px-5 py-3 text-sm shadow-sm">
          <summary className="cursor-pointer font-medium">Install details</summary>
          <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-xs">
            <dt className="text-subtle">Image</dt><dd className="break-all">{d.image || "-"}</dd>
            <dt className="text-subtle">Installer helper</dt><dd>{d.helper_ok ? "Available" : "Not found"}</dd>
            <dt className="text-subtle">Docker socket</dt><dd>{d.socket_ok ? "Enabled" : "Not enabled"}</dd>
            <dt className="text-subtle">One-click install</dt><dd>{d.can_apply ? "Ready" : d.apply_reason || "Not available"}</dd>
            <dt className="text-subtle">Status</dt><dd>{d.last_status || "idle"}</dd>
          </dl>
          {d.progress?.log && <pre className="mt-3 max-h-48 overflow-auto rounded-md bg-surface-2 p-3 font-mono text-[11px] leading-4 text-muted">{d.progress.log}</pre>}
        </details>
      </div>
    </div>
  );
}
