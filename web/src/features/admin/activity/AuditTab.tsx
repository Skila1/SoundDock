import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { relativeTime } from "@/lib/utils";
import { formatTimestamp, rangeSince, requestLogsHref, type TimeRange } from "./logFilters";

type AuditRow = {
  id: string;
  created_at: string;
  actor_user_id?: string | null;
  username?: string;
  display_name?: string;
  action: string;
  target?: string;
  ip?: string;
  meta?: Record<string, unknown>;
  request_id?: string;
};

type AuditPage = { items: AuditRow[]; next_cursor?: string };

const rangeOptions = [
  { value: "all", label: "All time" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" }
];

function actorLabel(r: AuditRow) {
  if (r.display_name && r.display_name !== r.username) return `${r.display_name} (${r.username})`;
  return r.username || (r.actor_user_id ? r.actor_user_id.slice(0, 8) : "System");
}

export function AuditTab() {
  const [draft, setDraft] = useState({ q: "", actor: "", ip: "" });
  const [f, setF] = useState(draft);
  const [range, setRange] = useState<TimeRange>("");
  const [open, setOpen] = useState<AuditRow | null>(null);
  useEffect(() => {
    const t = window.setTimeout(() => setF(draft), 400);
    return () => window.clearTimeout(t);
  }, [draft]);

  const q = useInfiniteQuery({
    queryKey: ["admin-audit", f, range],
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams({ limit: "50" });
      if (f.q.trim()) p.set("q", f.q.trim());
      if (f.actor.trim()) p.set("actor", f.actor.trim());
      if (f.ip.trim()) p.set("ip", f.ip.trim());
      if (range) p.set("since", rangeSince(range));
      if (pageParam) p.set("cursor", pageParam);
      return api.get<AuditPage>(`/api/v1/admin/audit?${p.toString()}`);
    },
    getNextPageParam: (last) => last.next_cursor || undefined
  });
  const items = (q.data?.pages || []).flatMap((p) => p.items || []);

  return (
    <div>
      <p className="mb-3 text-sm text-muted">Every change an administrator made: who, when, from where, and what changed.</p>
      <div className="mb-3 flex flex-wrap gap-2">
        <Input className="min-w-[14rem] flex-1" placeholder="Search actions and targets" value={draft.q} onChange={(e) => setDraft({ ...draft, q: e.target.value })} aria-label="Search audit log" />
        <Input className="w-48" placeholder="User" value={draft.actor} onChange={(e) => setDraft({ ...draft, actor: e.target.value })} aria-label="Filter by user" />
        <Input className="w-44" placeholder="IP address" value={draft.ip} onChange={(e) => setDraft({ ...draft, ip: e.target.value })} aria-label="Filter by IP" />
        <Select className="w-40" value={range || "all"} onValueChange={(v) => setRange((v === "all" ? "" : v) as TimeRange)} options={rangeOptions} />
      </div>
      <div className="overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2 text-muted">
            <tr><th className="p-3">When</th><th className="p-3">Who</th><th className="p-3">Change</th><th className="p-3">Target</th><th className="p-3">IP</th></tr>
          </thead>
          <tbody>
            {items.map((r) => (
              <tr key={r.id} className="cursor-pointer border-t border-border hover:bg-surface-2/60" onClick={() => setOpen(r)}>
                <td className="whitespace-nowrap p-3 text-muted" title={formatTimestamp(r.created_at)}>{relativeTime(r.created_at)}</td>
                <td className="p-3">{actorLabel(r)}</td>
                <td className="p-3 font-medium">{r.action}</td>
                <td className="max-w-[14rem] truncate p-3 text-muted">{r.target || "-"}</td>
                <td className="p-3 text-muted">{r.ip || "-"}</td>
              </tr>
            ))}
            {!items.length && (
              <tr><td colSpan={5} className="p-8 text-center text-muted">{q.isLoading ? "Loading…" : "No audit entries match."}</td></tr>
            )}
          </tbody>
        </table>
      </div>
      {q.hasNextPage && (
        <div className="mt-3 flex justify-center">
          <Button variant="secondary" disabled={q.isFetchingNextPage} onClick={() => q.fetchNextPage()}>{q.isFetchingNextPage ? "Loading…" : "Load older entries"}</Button>
        </div>
      )}
      <Dialog open={!!open} onOpenChange={(v) => { if (!v) setOpen(null); }}>
        <DialogContent title={open?.action || "Audit entry"} className="max-h-[90vh] overflow-y-auto">
          {open && (
            <div className="space-y-3 text-sm">
              <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-1.5">
                <dt className="text-muted">Time</dt><dd>{formatTimestamp(open.created_at)}</dd>
                <dt className="text-muted">User</dt><dd>{actorLabel(open)}</dd>
                <dt className="text-muted">IP address</dt><dd>{open.ip || "-"}</dd>
                <dt className="text-muted">Target</dt><dd className="break-all">{open.target || "-"}</dd>
              </dl>
              {open.meta && Object.keys(open.meta).length > 0 && (
                <pre className="max-h-64 overflow-auto rounded-lg bg-surface-2 p-3 font-mono text-[11px] text-muted">{JSON.stringify(open.meta, null, 2)}</pre>
              )}
              {open.request_id && (
                <Button asChild variant="secondary" size="sm">
                  <Link to={requestLogsHref(open.request_id)} onClick={() => setOpen(null)}>View the full request in Logs</Link>
                </Button>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
