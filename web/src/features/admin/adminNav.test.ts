import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { adminGroupFor, adminNavGroups, adminNavPaths, adminPath, legacyAdminRedirects } from "./adminNav";

const routerSource = readFileSync(resolve(__dirname, "../../app/router.tsx"), "utf8");

describe("admin navigation", () => {
  it("matches the agreed structure", () => {
    expect(adminNavGroups.map((g) => [g.label, g.links.map((l) => l.label)])).toEqual([
      ["Overview", ["Dashboard", "Activity"]],
      ["Library", ["Libraries", "Scan", "Catalog", "Media Settings"]],
      ["Storage", ["Storage", "Cleanup", "Archive", "Backups"]],
      ["IAM", ["Users", "Groups", "Access", "Discord", "Connected Services"]],
      ["System", ["Updates", "Integrations", "Discord Bot"]]
    ]);
  });

  it("gives every link an icon so the collapsed sidebar is readable", () => {
    for (const g of adminNavGroups) for (const l of g.links) expect(l.icon).toBeTruthy();
  });

  it("has a route for every nav link", () => {
    for (const g of adminNavGroups) {
      for (const l of g.links) {
        if (l.to === ".") {
          expect(routerSource).toContain("<Route index element={<AdminDashboard />} />");
        } else {
          expect(routerSource).toContain(`path="${l.to}"`);
        }
      }
    }
  });

  it("finds the bar button for a page", () => {
    expect(adminGroupFor("/admin").id).toBe("overview");
    expect(adminGroupFor("/admin/scan").id).toBe("library");
    expect(adminGroupFor("/admin/backups").id).toBe("storage");
    expect(adminGroupFor("/admin/users/1/library").id).toBe("iam");
    expect(adminGroupFor("/admin/updates").id).toBe("system");
    expect(adminGroupFor("/admin/connected").id).toBe("iam");
  });

  it("builds admin paths", () => {
    expect(adminPath(".")).toBe("/admin");
    expect(adminPath("activity")).toBe("/admin/activity");
    expect(new Set(adminNavPaths()).size).toBe(adminNavPaths().length);
  });

  it("redirects every retired page to a current page", () => {
    const current = new Set([...adminNavPaths(), "/admin/stats-migration"]);
    const navSegments = new Set(adminNavGroups.flatMap((g) => g.links.map((l) => l.to)));
    for (const [from, to] of Object.entries(legacyAdminRedirects)) {
      expect(navSegments.has(from), `${from} collides with a live page`).toBe(false);
      expect(current.has(to.split("?")[0]), `${from} -> ${to}`).toBe(true);
    }
    for (const retired of ["health", "diagnostics", "database", "workers", "inspect", "logs", "security", "grants", "quotas", "metadata", "lyrics", "transcoding", "acquisition-policy", "duplicate-review", "listen-compare", "stats-rebuild"]) {
      expect(legacyAdminRedirects[retired], retired).toBeTruthy();
    }
  });

  it("drops routes for pages that no longer exist", () => {
    for (const dead of ["cloudflare", "backup-preview", "demo"]) {
      expect(routerSource).not.toContain(`path="${dead}"`);
    }
  });
});
