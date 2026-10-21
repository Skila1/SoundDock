/** Pure helpers for the Activity page: tab routing and the merged log stream. */

export const activityTabs = ["activity", "logs"] as const;
export type ActivityTab = (typeof activityTabs)[number];

/** What the Logs stream shows: everything, errors from every source, or admin changes. */
export type ShowFilter = "" | "errors" | "admin";
export const showFilters: readonly ShowFilter[] = ["", "errors", "admin"];

export function showFromParams(params: URLSearchParams): ShowFilter {
  const v = params.get("show");
  return v === "errors" || v === "admin" ? v : "";
}

/**
 * Rewrites old ?tab= values (live, jobs, logs, audit, errors) to the new
 * tabs and Show filter. Returns null when the URL is already current.
 */
export function legacyActivityParams(params: URLSearchParams): URLSearchParams | null {
  const tab = params.get("tab");
  const next = new URLSearchParams(params);
  switch (tab) {
    case "live":
    case "jobs":
      next.delete("tab");
      return next;
    case "audit":
      next.set("tab", "logs");
      next.set("show", "admin");
      return next;
    case "errors":
      next.set("tab", "logs");
      next.set("show", "errors");
      return next;
    default:
      return null;
  }
}

export type LogEntry = {
  id: string;
  at: string;
  level: string;
  category: string;
  message: string;
  error?: string;
  summary?: string;
  type?: string;
  job_id?: string | null;
  library_id?: string | null;
  track_id?: string | null;
  actor_id?: string | null;
  actor_name?: string;
  request_id?: string;
  ip?: string;
  action?: string;
  method?: string;
  route?: string;
  status?: number | null;
  duration_ms?: number | null;
  result?: string;
  target?: string;
  details?: Record<string, unknown>;
  /** Set on rows that record an administrator changing something. */
  adminChange?: boolean;
};

export type AuditRow = {
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

export type ErrorRow = {
  id?: string;
  source?: string;
  class?: string;
  type?: string;
  category?: string;
  message?: string;
  last_error?: string;
  error?: string;
  at?: string;
  created_at?: string;
  status?: string;
  request_id?: string;
  job_id?: string;
  track_id?: string;
  [k: string]: unknown;
};

export const errorSources = [
  { value: "all", label: "All sources" },
  { value: "oplog", label: "Server errors" },
  { value: "job", label: "Jobs" },
  { value: "job_attempt", label: "Job attempts" },
  { value: "discord_playback", label: "Discord playback" },
  { value: "discord_gateway", label: "Discord bot" },
  { value: "acquisition", label: "Downloads" },
  { value: "scan_file", label: "Library scans" },
  { value: "webhook", label: "Webhooks" },
  { value: "external_account", label: "Provider accounts" },
  { value: "external_playlist", label: "Playlist imports" },
  { value: "external_sync", label: "Playlist sync" }
];

const sourceLabel: Record<string, string> = Object.fromEntries(errorSources.map((s) => [s.value, s.label]));

export function actorLabel(r: Pick<AuditRow, "display_name" | "username" | "actor_user_id">) {
  if (r.display_name && r.display_name !== r.username) return `${r.display_name} (${r.username})`;
  return r.username || (r.actor_user_id ? r.actor_user_id.slice(0, 8) : "System");
}

/** True for a successful write to an admin route: the same thing the audit log records. */
export function isAdminChange(e: Pick<LogEntry, "method" | "route" | "result">): boolean {
  if (!e.method || !e.route) return false;
  if (e.method === "GET" || e.method === "HEAD" || e.method === "OPTIONS") return false;
  return e.route.startsWith("/api/v1/admin/") && e.result !== "failure";
}

export function auditToEntry(r: AuditRow): LogEntry {
  const meta = r.meta || {};
  return {
    id: `audit-${r.id}`,
    at: r.created_at,
    level: "info",
    category: "admin change",
    message: r.action,
    actor_id: r.actor_user_id || null,
    actor_name: actorLabel(r),
    ip: r.ip,
    request_id: r.request_id,
    method: typeof meta.method === "string" ? meta.method : undefined,
    route: typeof meta.route === "string" ? meta.route : undefined,
    status: typeof meta.status === "number" ? meta.status : null,
    target: r.target,
    details: meta,
    adminChange: true
  };
}

export function errorToEntry(r: ErrorRow, i: number): LogEntry {
  const at = r.at || r.created_at || "";
  const { id, source, class: cls, message, last_error, error, at: _at, created_at: _c, request_id, job_id, track_id, level: _l, ...rest } = r;
  return {
    id: `error-${id || `${source}-${at}-${i}`}`,
    at,
    level: "error",
    category: sourceLabel[source || ""] || source || r.type || "error",
    message: String(message || last_error || error || "Unknown error"),
    summary: typeof cls === "string" && cls !== source ? cls : undefined,
    request_id,
    job_id: job_id || null,
    track_id: track_id || null,
    details: rest
  };
}
