import { describe, expect, it } from "vitest";
import {
  activeFilterCount,
  emptyLogFilters,
  filtersFromParams,
  filtersToParams,
  formatDurationMs,
  levelTone,
  logsQuery,
  rangeSince,
  requestLogsHref,
  severityLevels
} from "./logFilters";

const NOW = Date.UTC(2026, 9, 2, 12, 0, 0);

describe("activity log filters", () => {
  it("maps severities to server levels", () => {
    expect(severityLevels("")).toBe("");
    expect(severityLevels("warn+")).toBe("warn,error");
    expect(severityLevels("error")).toBe("error");
  });

  it("builds the logs query with only the filters in use", () => {
    const q = new URLSearchParams(
      logsQuery({ ...emptyLogFilters, q: " scan ", severity: "warn+", result: "failure", category: "library", actor: "alice", ip: "203.0.113", requestId: "r-1", range: "24h" }, "cur|x", 25, NOW)
    );
    expect(q.get("q")).toBe("scan");
    expect(q.get("level")).toBe("warn,error");
    expect(q.get("result")).toBe("failure");
    expect(q.get("category")).toBe("library");
    expect(q.get("actor")).toBe("alice");
    expect(q.get("ip")).toBe("203.0.113");
    expect(q.get("request_id")).toBe("r-1");
    expect(q.get("since")).toBe(new Date(NOW - 86_400_000).toISOString());
    expect(q.get("cursor")).toBe("cur|x");
    expect(q.get("limit")).toBe("25");

    const bare = new URLSearchParams(logsQuery(emptyLogFilters, "", 50, NOW));
    expect([...bare.keys()]).toEqual(["limit"]);
  });

  it("round-trips filters through the URL and keeps other params", () => {
    const base = new URLSearchParams("tab=logs");
    const f = { ...emptyLogFilters, q: "login", severity: "error" as const, actor: "bob", requestId: "abc", range: "7d" as const };
    const p = filtersToParams(f, base);
    expect(p.get("tab")).toBe("logs");
    expect(filtersFromParams(p)).toEqual(f);
    const cleared = filtersToParams(emptyLogFilters, p);
    expect(cleared.toString()).toBe("tab=logs");
  });

  it("ignores unknown values from the URL", () => {
    const f = filtersFromParams(new URLSearchParams("severity=loud&result=maybe&range=1y"));
    expect(f.severity).toBe("");
    expect(f.result).toBe("");
    expect(f.range).toBe("");
  });

  it("counts active filters", () => {
    expect(activeFilterCount(emptyLogFilters)).toBe(0);
    expect(activeFilterCount({ ...emptyLogFilters, ip: "1.2.3.4", result: "success" })).toBe(2);
  });

  it("links to every entry from one request", () => {
    expect(requestLogsHref("a/b 1")).toBe("/admin/activity?tab=logs&request=a%2Fb+1");
  });

  it("formats helpers", () => {
    expect(rangeSince("", NOW)).toBe("");
    expect(rangeSince("1h", NOW)).toBe(new Date(NOW - 3600_000).toISOString());
    expect(formatDurationMs(null)).toBe("");
    expect(formatDurationMs(42)).toBe("42 ms");
    expect(formatDurationMs(2500)).toBe("2.5 s");
    expect(formatDurationMs(125_000)).toBe("2 min");
    expect(levelTone("error")).toBe("danger");
    expect(levelTone("warn")).toBe("warning");
  });
});
