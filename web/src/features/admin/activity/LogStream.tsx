import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Copy, Filter, Pause, Play, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { cn, relativeTime } from "@/lib/utils";
import {
  activeFilterCount,
  emptyLogFilters,
  filtersFromParams,
  filtersToParams,
  formatDurationMs,
  formatTimestamp,
  levelTone,
  logsQuery,
  rangeSince,
  type LogFilters
} from "./logFilters";
import {
  auditToEntry,
  errorSources,
  errorToEntry,
  isAdminChange,
  showFromParams,
  type AuditRow,
  type ErrorRow,
  type LogEntry,
  type ShowFilter
} from "./streamModel";

type LogsPage = { items: LogEntry[]; next_cursor?: string; categories?: string[]; dropped?: number };
type AuditPage = { items: AuditRow[]; next_cursor?: string };

const showOptions: { value: ShowFilter; label: string }[] = [
  { value: "", label: "All" },
  { value: "errors", label: "Errors" },
  { value: "admin", label: "Admin changes" }
];

const severityOptions = [
  { value: "all", label: "All severities" },
  { value: "error", label: "Errors" },
  { value: "warn+", label: "Warnings and errors" },
  { value: "warn", label: "Warnings" },
  { value: "info", label: "Info" },
  { value: "debug", label: "Debug" }
];

const resultOptions = [
  { value: "all", label: "Any result" },
  { value: "success", label: "Succeeded" },
  { value: "failure", label: "Failed" }
];

const rangeOptions = [
  { value: "all", label: "All time" },
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" }
];

const LIVE_MS = 5000;
// Filters that only make sense for one Show mode; dropped when switching.
const modeParams = ["severity", "result", "category", "request", "source"];

/** One stream for server logs, errors from every source, and admin changes. */
export function LogStream() {
  const [params, setParams] = useSearchParams();
  const show = showFromParams(params);
  const filters = useMemo(() => filtersFromParams(params), [params]);
  const source = params.get("source") || "all";
  const [draft, setDraft] = useState({ q: filters.q, actor: filters.actor, ip: filters.ip });
  const [live, setLive] = useState(true);
  const [open, setOpen] = useState<LogEntry | null>(null);
  const [showMore, setShowMore] = useState(!!(filters.actor || filters.ip));

  useEffect(() => {
    setDraft({ q: filters.q, actor: filters.actor, ip: filters.ip });
  }, [filters.q, filters.actor, filters.ip]);

  const apply = (patch: Partial<LogFilters>) => {
    setParams(filtersToParams({ ...filters, ...patch }, params), { replace: true });
  };

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next, { replace: true });
  };

  const setShow = (s: ShowFilter) => {
    const next = new URLSearchParams(params);
    modeParams.forEach((k) => next.delete(k));
    if (s) next.set("show", s);
    else next.delete("show");
    setParams(next, { replace: true });
  };

  // Text filters apply after a short pause so typing does not spam the API.
  useEffect(() => {
    const t = window.setTimeout(() => {
      if (draft.q !== filters.q || draft.actor !== filters.actor || draft.ip !== filters.ip) apply(draft);
    }, 400);
    return () => window.clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  const logs = useInfiniteQuery({
    queryKey: ["admin-logs", filters],
    enabled: show === "",
    initialPageParam: "",
    queryFn: ({ pageParam }) => api.get<LogsPage>(`/api/v1/admin/logs?${logsQuery(filters, pageParam)}`),
    getNextPageParam: (last) => last.next_cursor || undefined,
    // Live refresh only while the first page is shown; paging back stays stable.
    refetchInterval: (query) => (live && (query.state.data?.pages.length ?? 0) <= 1 ? LIVE_MS : false)
  });

  const audit = useInfiniteQuery({
    queryKey: ["admin-audit", filters.q, filters.actor, filters.ip, filters.range],
    enabled: show === "admin",
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams({ limit: "50" });
      if (filters.q.trim()) p.set("q", filters.q.trim());
      if (filters.actor.trim()) p.set("actor", filters.actor.trim());
      if (filters.ip.trim()) p.set("ip", filters.ip.trim());
      if (filters.range) p.set("since", rangeSince(filters.range));
      if (pageParam) p.set("cursor", pageParam);
      return api.get<AuditPage>(`/api/v1/admin/audit?${p.toString()}`);
    },
    getNextPageParam: (last) => last.next_cursor || undefined,
    refetchInterval: (query) => (live && (query.state.data?.pages.length ?? 0) <= 1 ? LIVE_MS * 3 : false)
  });

  const errors = useQuery({
    queryKey: ["admin-errors", source, filters.q],
    enabled: show === "errors",
    queryFn: () => {
      const p = new URLSearchParams({ limit: "200" });
      if (source !== "all") p.set("source", source);
      if (filters.q.trim()) p.set("q", filters.q.trim());
      return api.get<{ items?: ErrorRow[] }>(`/api/v1/admin/errors?${p.toString()}`);
    },
    refetchInterval: live ? LIVE_MS * 3 : false
  });

  const items: LogEntry[] = useMemo(() => {
    if (show === "admin") return (audit.data?.pages || []).flatMap((p) => p.items || []).map(auditToEntry);
    if (show === "errors") {
      const since = filters.range ? Date.parse(rangeSince(filters.range)) : 0;
      return (errors.data?.items || []).map(errorToEntry).filter((e) => !since || Date.parse(e.at) >= since);
    }
    return (logs.data?.pages || []).flatMap((p) => p.items || []).map((e) => (isAdminChange(e) ? { ...e, adminChange: true } : e));
  }, [show, audit.data, errors.data, logs.data, filters.range]);

  const active = show === "admin" ? audit : show === "errors" ? errors : logs;
  const paged = show === "admin" ? audit : show === "" ? logs : null;
  const firstLogs = logs.data?.pages[0];
  const categories = firstLogs?.categories || [];
  const dropped = show === "" ? firstLogs?.dropped || 0 : 0;
  const count = activeFilterCount(filters) + (source !== "all" ? 1 : 0);
  const peopleFilters = show !== "errors";

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div role="radiogroup" aria-label="Show" className="flex w-full rounded-full bg-surface-2 p-1 sm:w-auto">
          {showOptions.map((o) => (
            <button
              key={o.value || "all"}
              type="button"
              role="radio"
              aria-checked={show === o.value}
              onClick={() => setShow(o.value)}
              className={cn(
                "flex-1 whitespace-nowrap rounded-full px-3 py-1.5 text-xs font-semibold transition-colors sm:flex-none",
                show === o.value ? "bg-surface-1 text-foreground shadow-sm" : "text-muted hover:text-foreground"
              )}
            >
              {o.label}
            </button>
          ))}
        </div>
        <Input
          className="min-w-0 flex-1 basis-56"
          placeholder={show === "admin" ? "Search changes and targets…" : show === "errors" ? "Search errors…" : "Search messages, routes, users, request IDs…"}
          value={draft.q}
          onChange={(e) => setDraft({ ...draft, q: e.target.value })}
          aria-label="Search activity"
        />
        <Button variant={live ? "secondary" : "ghost"} size="sm" onClick={() => setLive((v) => !v)} aria-pressed={live} title="Refresh every few seconds">
          {live ? <Pause /> : <Play />} {live ? "Live" : "Paused"}
        </Button>
      </div>

      <div className="mb-3 grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
        {show === "" && (
          <>
            <Select className="sm:w-48" value={filters.severity || "all"} onValueChange={(v) => apply({ severity: (v === "all" ? "" : v) as LogFilters["severity"] })} options={severityOptions} />
            <Select className="sm:w-36" value={filters.result || "all"} onValueChange={(v) => apply({ result: (v === "all" ? "" : v) as LogFilters["result"] })} options={resultOptions} />
            <Select
              className="sm:w-40"
              value={filters.category || "all"}
              onValueChange={(v) => apply({ category: v === "all" ? "" : v })}
              options={[{ value: "all", label: "All categories" }, ...Array.from(new Set([...categories, ...(filters.category ? [filters.category] : [])])).map((c) => ({ value: c, label: c }))]}
            />
          </>
        )}
        {show === "errors" && <Select className="sm:w-52" value={source} onValueChange={(v) => setParam("source", v === "all" ? "" : v)} options={errorSources} />}
        <Select className="sm:w-40" value={filters.range || "all"} onValueChange={(v) => apply({ range: (v === "all" ? "" : v) as LogFilters["range"] })} options={rangeOptions} />
        {peopleFilters && (
          <Button variant="ghost" size="sm" className="h-10" onClick={() => setShowMore((v) => !v)} aria-expanded={showMore}>
            <Filter /> User & IP
          </Button>
        )}
      </div>
      {peopleFilters && showMore && (
        <div className="mb-3 flex flex-wrap gap-2">
          <Input className="min-w-0 flex-1 basis-40 sm:w-56 sm:flex-none" placeholder="User name or ID" value={draft.actor} onChange={(e) => setDraft({ ...draft, actor: e.target.value })} aria-label="Filter by user" />
          <Input className="min-w-0 flex-1 basis-40 sm:w-56 sm:flex-none" placeholder="IP address or prefix" value={draft.ip} onChange={(e) => setDraft({ ...draft, ip: e.target.value })} aria-label="Filter by IP" />
        </div>
      )}
      {(count > 0 || filters.requestId) && (
        <div className="mb-3 flex flex-wrap items-center gap-2 text-sm">
          {filters.requestId && (
            <Badge tone="accent">
              Request {filters.requestId.slice(-12)}
              <button className="ml-1" aria-label="Clear request filter" onClick={() => apply({ requestId: "" })}><X className="h-3 w-3" /></button>
            </Badge>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setDraft({ q: "", actor: "", ip: "" });
              const next = filtersToParams(emptyLogFilters, params);
              next.delete("source");
              setParams(next, { replace: true });
            }}
          >
            Clear filters
          </Button>
        </div>
      )}
      {dropped > 0 && <p className="mb-3 text-xs text-warning">{dropped} entries were skipped since start-up because the log buffer was full.</p>}

      <div className="overflow-hidden rounded-xl border border-border">
        <ul className="divide-y divide-border">
          {items.map((e) => (
            <li key={e.id}>
              <button type="button" className="grid w-full grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 px-4 py-2.5 text-left hover:bg-surface-2/60 sm:grid-cols-[9rem_minmax(0,1fr)_auto]" onClick={() => setOpen(e)}>
                <span className="text-xs text-subtle sm:pt-0.5" title={formatTimestamp(e.at)}>{relativeTime(e.at)}</span>
                <span className="min-w-0">
                  <span className="flex flex-wrap items-center gap-2">
                    {e.adminChange ? <Badge tone="accent">admin change</Badge> : <Badge tone={levelTone(e.level)}>{e.level}</Badge>}
                    {e.category !== "admin change" && <span className="text-xs text-muted">{e.category}</span>}
                    {e.result === "failure" && e.level === "info" && <Badge tone="warning">failed</Badge>}
                  </span>
                  <span className={cn("mt-0.5 block truncate text-sm", e.level === "error" && "text-destructive")}>{e.message || e.summary || e.action}</span>
                  <span className="block truncate text-xs text-subtle">
                    {[e.actor_name, e.target, e.ip, e.method && e.route ? `${e.method} ${e.route}` : ""].filter(Boolean).join(" · ")}
                  </span>
                </span>
                <span className="col-start-2 flex items-center gap-2 text-xs text-muted sm:col-start-3 sm:justify-end">
                  {e.status != null && <span className={cn(e.status >= 500 ? "text-destructive" : e.status >= 400 ? "text-warning" : "")}>{e.status}</span>}
                  {e.duration_ms != null && <span>{formatDurationMs(e.duration_ms)}</span>}
                </span>
              </button>
            </li>
          ))}
          {!items.length && (
            <li className="px-4 py-10 text-center text-sm text-muted">
              {active.isLoading
                ? "Loading activity…"
                : active.isError
                  ? "Could not load activity."
                  : count
                    ? "Nothing matches these filters."
                    : show === "errors"
                      ? "No errors. Nice."
                      : show === "admin"
                        ? "No admin changes recorded yet."
                        : "No activity recorded yet."}
            </li>
          )}
        </ul>
      </div>
      {paged?.hasNextPage && (
        <div className="mt-3 flex justify-center">
          <Button variant="secondary" disabled={paged.isFetchingNextPage} onClick={() => paged.fetchNextPage()}>
            {paged.isFetchingNextPage ? "Loading…" : "Load older entries"}
          </Button>
        </div>
      )}

      <LogDetail
        entry={open}
        onClose={() => setOpen(null)}
        onFilter={(patch) => {
          setOpen(null);
          const next = filtersToParams({ ...emptyLogFilters, ...patch }, params);
          modeParams.filter((k) => k !== "request").forEach((k) => next.delete(k));
          next.delete("show");
          setParams(next, { replace: true });
          if (patch.actor || patch.ip) setShowMore(true);
        }}
      />
    </div>
  );
}

function LogDetail({ entry, onClose, onFilter }: { entry: LogEntry | null; onClose: () => void; onFilter: (patch: Partial<LogFilters>) => void }) {
  const e = entry;
  const details = (e?.details || {}) as Record<string, unknown>;
  // The error is already shown above, so leave it out of the raw details.
  const { stack, error: _error, ...rest } = details;
  return (
    <Sheet open={!!e} onOpenChange={(v) => { if (!v) onClose(); }}>
      <SheetContent title={e?.adminChange ? "Admin change" : "Activity entry"} className="flex w-[min(560px,96vw)] flex-col overflow-y-auto">
        {e && (
          <div className="space-y-4 text-sm">
            <div>
              <div className="mb-1 flex flex-wrap items-center gap-2">
                {e.adminChange ? <Badge tone="accent">admin change</Badge> : <Badge tone={levelTone(e.level)}>{e.level}</Badge>}
                {e.category !== "admin change" && <Badge>{e.category}</Badge>}
                {e.result && <Badge tone={e.result === "failure" ? "danger" : "success"}>{e.result === "failure" ? "Failed" : "Succeeded"}</Badge>}
              </div>
              <p className="break-words font-medium">{e.message}</p>
              {e.error && !e.message.includes(e.error) && <p className="mt-1 break-words text-destructive">{e.error}</p>}
              {e.summary && <p className="mt-1 text-muted">{e.summary}</p>}
            </div>
            <dl className="grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)] gap-x-3 gap-y-1.5">
              <dt className="text-muted">Time</dt><dd>{formatTimestamp(e.at)}</dd>
              {e.actor_name || e.actor_id ? (
                <>
                  <dt className="text-muted">User</dt>
                  <dd className="flex flex-wrap items-center gap-2">
                    {e.actor_name || e.actor_id}
                    <button className="text-xs text-accent hover:underline" onClick={() => onFilter({ actor: e.actor_id || e.actor_name || "" })}>All from this user</button>
                  </dd>
                </>
              ) : null}
              {e.ip ? (
                <>
                  <dt className="text-muted">IP address</dt>
                  <dd className="flex flex-wrap items-center gap-2">
                    {e.ip}
                    <button className="text-xs text-accent hover:underline" onClick={() => onFilter({ ip: e.ip || "" })}>All from this IP</button>
                  </dd>
                </>
              ) : null}
              {e.target ? (<><dt className="text-muted">Target</dt><dd className="break-all">{e.target}</dd></>) : null}
              {e.method || e.route ? (<><dt className="text-muted">Request</dt><dd className="break-all font-mono text-xs">{e.method} {e.route}</dd></>) : null}
              {e.status != null ? (<><dt className="text-muted">Status</dt><dd>{e.status}{e.duration_ms != null ? ` · ${formatDurationMs(e.duration_ms)}` : ""}</dd></>) : e.duration_ms != null ? (<><dt className="text-muted">Duration</dt><dd>{formatDurationMs(e.duration_ms)}</dd></>) : null}
              {e.action ? (<><dt className="text-muted">Action</dt><dd className="break-all font-mono text-xs">{e.action}</dd></>) : null}
              {e.request_id ? (
                <>
                  <dt className="text-muted">Request ID</dt>
                  <dd className="flex flex-wrap items-center gap-2">
                    <span className="break-all font-mono text-xs">{e.request_id}</span>
                    <button aria-label="Copy request ID" className="text-muted hover:text-foreground" onClick={() => { void navigator.clipboard?.writeText(e.request_id || ""); toast.success("Request ID copied"); }}><Copy className="h-3.5 w-3.5" /></button>
                    <button className="text-xs text-accent hover:underline" onClick={() => onFilter({ requestId: e.request_id || "" })}>Everything from this request</button>
                  </dd>
                </>
              ) : null}
              {e.job_id ? (<><dt className="text-muted">Job</dt><dd className="break-all font-mono text-xs">{e.job_id}</dd></>) : null}
              {e.track_id ? (<><dt className="text-muted">Track</dt><dd><Link className="text-accent hover:underline" to={`/tracks/${e.track_id}`}>Open track</Link></dd></>) : null}
              {e.library_id ? (<><dt className="text-muted">Library</dt><dd className="break-all font-mono text-xs">{e.library_id}</dd></>) : null}
            </dl>
            {Object.keys(rest).length > 0 && (
              <div>
                <h3 className="mb-1 font-medium">Details</h3>
                <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-surface-2 p-3 font-mono text-[11px] leading-4 text-muted">{JSON.stringify(rest, null, 2)}</pre>
              </div>
            )}
            {typeof stack === "string" && (
              <details>
                <summary className="cursor-pointer font-medium">Stack trace</summary>
                <pre className="mt-2 max-h-72 overflow-auto rounded-lg bg-surface-2 p-3 font-mono text-[11px] leading-4 text-muted">{stack}</pre>
              </details>
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}
