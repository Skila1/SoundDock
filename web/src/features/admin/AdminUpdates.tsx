import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge, Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { relativeTime } from "@/lib/utils";

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
  const updating = !!(d.updating || d.last_status === "updating" || d.progress?.stage === "pulling" || d.progress?.stage === "restarting" || d.progress?.stage === "queued");
  const changelog = Array.isArray(d.changelog) ? d.changelog : [];
  const pct = typeof d.progress?.percent === "number" ? d.progress.percent : updating ? 12 : 0;

  useEffect(() => {
    if (!watch) return;
    if (updating) return;
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

  return (
    <div>
      <PageHeader title="Updates" description="Check for new SoundDock releases and install them. Your database keeps running during an update." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.available ? "warning" : "success"}>{d.available ? "Update available" : "Up to date"}</Badge>
        {updating ? <Badge tone="accent">Updating</Badge> : d.can_apply ? <Badge tone="success">Ready to install</Badge> : <Badge tone="warning">Cannot install from here</Badge>}
        {d.schema_forward_only ? <Badge tone="warning">Cannot be rolled back</Badge> : null}
        {d.needs_recovery ? <Badge tone="danger">Needs recovery</Badge> : null}
      </div>
      <div className="mb-4 rounded-2xl border border-border bg-surface-1 p-6 shadow-sm">
        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <div className="text-sm text-muted">Installed</div>
            <div className="text-2xl font-semibold">{d.version || "0.0.1"}</div>
          </div>
          <div>
            <div className="text-sm text-muted">Latest</div>
            <div className="text-2xl font-semibold">{d.latest_version || d.version || "0.0.1"}</div>
          </div>
        </div>
        <p className="mt-3 text-sm text-muted">Last check: {d.last_check_at ? relativeTime(d.last_check_at) : "never"}</p>
        <p className="text-sm text-muted">Last update: {d.last_applied_at ? relativeTime(d.last_applied_at) : "never"}{d.last_applied_by ? ` (${d.last_applied_by})` : ""}</p>
        {d.last_error && <p className="mt-2 text-sm text-destructive">{d.last_error}</p>}
        {d.needs_recovery && (
          <p className="mt-2 text-sm text-destructive">
            This update changed the database and then failed, so the previous version was not restarted. Restore the backup taken before the update{d.backup_path ? ` (${d.backup_path})` : ""} from Backups.
          </p>
        )}
        {d.apply_reason && <p className="mt-2 text-sm text-muted">{d.apply_reason}</p>}
        {!d.can_apply && !d.apply_reason && <p className="mt-2 text-sm text-muted">This server cannot install updates by itself. Re-run the SoundDock installer on the host to enable one-click updates.</p>}

        {d.available && !updating && (
          <div className="mt-4 rounded-lg border border-border bg-surface-2 p-4">
            <div className="text-sm font-medium">
              {d.latest_version && d.latest_version !== d.version ? `Version ${d.latest_version}` : "Newer version"}
            </div>
            {changelog.length ? (
              <ul className="mt-3 space-y-3">
                {changelog.map((rel: { version: string; notes: string[] }) => (
                  <li key={rel.version}>
                    <div className="text-xs font-semibold text-muted">{rel.version}</div>
                    <ul className="mt-1 list-disc space-y-1 pl-5 text-sm">
                      {(rel.notes || []).map((n: string, i: number) => <li key={i}>{n}</li>)}
                    </ul>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-muted">A newer version is ready. Release notes appear once SoundDock can reach GitHub.</p>
            )}
          </div>
        )}

        {updating && (
          <div className="mt-4 space-y-2">
            <div className="flex items-center justify-between text-sm">
              <span className="font-medium">{d.progress?.stage === "restarting" ? "Starting new containers" : d.progress?.stage === "queued" ? "Waiting for the host" : "Downloading"}</span>
              <span className="text-muted">{pct}%</span>
            </div>
            <Progress value={pct} />
            <p className="text-xs text-subtle">{d.progress?.detail || "Downloading the new version."}</p>
            {d.progress?.log && (
              <pre className="max-h-36 overflow-auto rounded-md bg-surface-2 p-3 font-mono text-[11px] leading-4 text-muted">{d.progress.log}</pre>
            )}
          </div>
        )}

        <details className="mt-3 text-xs text-subtle">
          <summary className="cursor-pointer">How updates are installed</summary>
          <p className="mt-1 break-all">Image: {d.image || "-"}</p>
          <p>Installer helper: {d.helper_ok ? "available" : "not found"} · Docker socket: {d.socket_ok ? "enabled" : "not enabled"}</p>
          <p>Status: {d.last_status || "idle"}</p>
        </details>

        <div className="mt-4 flex flex-wrap gap-2">
          <Button variant="secondary" disabled={busy || d.checking || updating} onClick={async () => {
            setBusy(true);
            try {
              await api.post("/api/v1/admin/updates/check");
              qc.invalidateQueries({ queryKey: ["admin-updates"] });
              toast.success("Checked for updates");
            } catch {
              toast.error("Could not check for updates");
            } finally {
              setBusy(false);
            }
          }}>Check now</Button>
          <Button disabled={busy || updating || !d.can_apply || !d.available || d.needs_recovery} onClick={async () => {
            setBusy(true);
            try {
              await api.post("/api/v1/admin/updates/apply");
              setWatch(true);
              toast.success("Update started");
              qc.invalidateQueries({ queryKey: ["admin-updates"] });
            } catch (e: any) {
              toast.error(e?.message || "Could not start update");
            } finally {
              setBusy(false);
            }
          }}>{updating ? "Updating…" : "Update now"}</Button>
        </div>
      </div>
      <div className="flex max-w-lg items-center justify-between rounded-2xl border border-border bg-surface-1 p-5 shadow-sm">
        <div>
          <div className="text-sm font-medium">Automatic updates</div>
          <p className="text-xs text-subtle">Checks about once an hour and installs new versions automatically.</p>
        </div>
        <Switch checked={!!d.auto_enabled} onCheckedChange={(v) => api.put("/api/v1/admin/updates", { auto_enabled: v }).then(() => { toast.success(v ? "Automatic updates on" : "Automatic updates off"); qc.invalidateQueries({ queryKey: ["admin-updates"] }); })} />
      </div>
    </div>
  );
}
