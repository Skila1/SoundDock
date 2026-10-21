import { describe, expect, it } from "vitest";
import { auditToEntry, errorToEntry, isAdminChange, legacyActivityParams, showFromParams } from "./streamModel";

describe("activity tabs", () => {
  it("maps old tab values to the new tabs and Show filter", () => {
    expect(legacyActivityParams(new URLSearchParams("tab=live"))?.toString()).toBe("");
    expect(legacyActivityParams(new URLSearchParams("tab=jobs"))?.toString()).toBe("");
    expect(legacyActivityParams(new URLSearchParams("tab=audit&q=x"))?.toString()).toBe("tab=logs&q=x&show=admin");
    expect(legacyActivityParams(new URLSearchParams("tab=errors"))?.toString()).toBe("tab=logs&show=errors");
    expect(legacyActivityParams(new URLSearchParams("tab=logs&request=r1"))).toBeNull();
    expect(legacyActivityParams(new URLSearchParams(""))).toBeNull();
  });

  it("reads the Show filter", () => {
    expect(showFromParams(new URLSearchParams("show=errors"))).toBe("errors");
    expect(showFromParams(new URLSearchParams("show=admin"))).toBe("admin");
    expect(showFromParams(new URLSearchParams("show=nope"))).toBe("");
  });
});

describe("log stream rows", () => {
  it("tags successful admin writes", () => {
    expect(isAdminChange({ method: "PUT", route: "/api/v1/admin/lyrics", result: "success" })).toBe(true);
    expect(isAdminChange({ method: "GET", route: "/api/v1/admin/lyrics", result: "success" })).toBe(false);
    expect(isAdminChange({ method: "PUT", route: "/api/v1/admin/lyrics", result: "failure" })).toBe(false);
    expect(isAdminChange({ method: "POST", route: "/api/v1/me/password", result: "success" })).toBe(false);
  });

  it("turns audit and error rows into stream entries", () => {
    const a = auditToEntry({ id: "1", created_at: "2026-10-01T00:00:00Z", username: "ali", action: "Update user", target: "u1", meta: { method: "PATCH", status: 200 } });
    expect(a).toMatchObject({ id: "audit-1", adminChange: true, actor_name: "ali", method: "PATCH", status: 200, target: "u1" });
    const e = errorToEntry({ source: "acquisition", message: "HTTP Error 403", at: "2026-10-01T00:00:00Z", status: "failed" }, 0);
    expect(e).toMatchObject({ level: "error", category: "Downloads", message: "HTTP Error 403" });
    expect(e.details).toEqual({ status: "failed" });
  });
});
