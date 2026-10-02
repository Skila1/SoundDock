/** Filters shared by the Activity Logs tab and the URL. */
export type Severity = "" | "error" | "warn+" | "warn" | "info" | "debug";
export type ResultFilter = "" | "success" | "failure";
export type TimeRange = "" | "1h" | "24h" | "7d" | "30d";

export type LogFilters = {
  q: string;
  severity: Severity;
  result: ResultFilter;
  category: string;
  actor: string;
  ip: string;
  requestId: string;
  range: TimeRange;
};

export const emptyLogFilters: LogFilters = {
  q: "",
  severity: "",
  result: "",
  category: "",
  actor: "",
  ip: "",
  requestId: "",
  range: ""
};

const SEVERITIES: Severity[] = ["", "error", "warn+", "warn", "info", "debug"];
const RESULTS: ResultFilter[] = ["", "success", "failure"];
const RANGES: TimeRange[] = ["", "1h", "24h", "7d", "30d"];

const RANGE_MS: Record<Exclude<TimeRange, "">, number> = {
  "1h": 3600_000,
  "24h": 86_400_000,
  "7d": 7 * 86_400_000,
  "30d": 30 * 86_400_000
};

/** Server-side level list for a severity choice. */
export function severityLevels(s: Severity): string {
  switch (s) {
    case "warn+":
      return "warn,error";
    case "":
      return "";
    default:
      return s;
  }
}

export function rangeSince(range: TimeRange, now: number = Date.now()): string {
  if (!range) return "";
  return new Date(now - RANGE_MS[range]).toISOString();
}

/** Query string for GET /api/v1/admin/logs. */
export function logsQuery(f: LogFilters, cursor = "", limit = 50, now: number = Date.now()): string {
  const p = new URLSearchParams();
  const put = (k: string, v: string) => {
    const t = v.trim();
    if (t) p.set(k, t);
  };
  put("q", f.q);
  put("level", severityLevels(f.severity));
  put("result", f.result);
  put("category", f.category);
  put("actor", f.actor);
  put("ip", f.ip);
  put("request_id", f.requestId);
  put("since", rangeSince(f.range, now));
  put("cursor", cursor);
  p.set("limit", String(limit));
  return p.toString();
}

/** Reads filters from the page URL (unknown values fall back to empty). */
export function filtersFromParams(params: URLSearchParams): LogFilters {
  const pick = <T extends string>(v: string | null, allowed: T[]): T => (v && allowed.includes(v as T) ? (v as T) : ("" as T));
  return {
    q: params.get("q") || "",
    severity: pick(params.get("severity"), SEVERITIES),
    result: pick(params.get("result"), RESULTS),
    category: params.get("category") || "",
    actor: params.get("actor") || "",
    ip: params.get("ip") || "",
    requestId: params.get("request") || "",
    range: pick(params.get("range"), RANGES)
  };
}

/** Writes filters into the page URL, keeping unrelated params (such as tab). */
export function filtersToParams(f: LogFilters, base: URLSearchParams): URLSearchParams {
  const p = new URLSearchParams(base);
  const set = (k: string, v: string) => {
    if (v.trim()) p.set(k, v.trim());
    else p.delete(k);
  };
  set("q", f.q);
  set("severity", f.severity);
  set("result", f.result);
  set("category", f.category);
  set("actor", f.actor);
  set("ip", f.ip);
  set("request", f.requestId);
  set("range", f.range);
  return p;
}

export function activeFilterCount(f: LogFilters): number {
  return (Object.keys(emptyLogFilters) as (keyof LogFilters)[]).filter((k) => f[k] !== "").length;
}

/** Link to the Logs tab showing every entry from one request. */
export function requestLogsHref(requestId: string): string {
  const p = new URLSearchParams({ tab: "logs", request: requestId });
  return `/admin/activity?${p.toString()}`;
}

export function levelTone(level?: string) {
  if (level === "error") return "danger" as const;
  if (level === "warn") return "warning" as const;
  if (level === "debug") return "neutral" as const;
  return "accent" as const;
}

export function formatDurationMs(ms?: number | null): string {
  if (ms == null) return "";
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`;
  return `${Math.round(ms / 60_000)} min`;
}

export function formatTimestamp(iso?: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, { year: "numeric", month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
