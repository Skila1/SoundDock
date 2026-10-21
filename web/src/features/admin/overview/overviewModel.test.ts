import { describe, expect, it } from "vitest";
import { attentionItems, backupTone, isYouTubeBlock, recentErrors, storageUsed } from "./overviewModel";

const NOW = Date.UTC(2026, 9, 22, 12, 0, 0);
const iso = (msAgo: number) => new Date(NOW - msAgo).toISOString();

describe("overview model", () => {
  it("keeps only errors from the last day", () => {
    const items = [{ at: iso(3600_000) }, { at: iso(2 * 86_400_000) }, { at: "" }];
    expect(recentErrors(items, NOW)).toHaveLength(1);
  });

  it("spots YouTube blocking", () => {
    expect(isYouTubeBlock({ message: "ERROR: Sign in to confirm you're not a bot" })).toBe(true);
    expect(isYouTubeBlock({ message: "HTTP Error 403: Forbidden" })).toBe(true);
    expect(isYouTubeBlock({ message: "Video unavailable" })).toBe(false);
  });

  it("lists only what needs doing", () => {
    expect(attentionItems({ failedJobs24h: 0, failedDownloads24h: 0, youtubeBlocked24h: 0, passphraseMissing: false, statsCutoverPending: false })).toEqual([]);
    const items = attentionItems({ failedJobs24h: 2, failedDownloads24h: 3, youtubeBlocked24h: 3, passphraseMissing: true, statsCutoverPending: true });
    expect(items.map((i) => i.id)).toEqual(["youtube", "jobs", "passphrase", "stats"]);
    expect(items[1].title).toBe("2 jobs failed today");
  });

  it("sums storage and grades backups", () => {
    expect(storageUsed([])).toBeNull();
    expect(storageUsed([{ used_bytes: 10, total_bytes: 100 }, { used_bytes: 5 }])).toEqual({ used: 15, total: 100 });
    expect(backupTone(undefined, NOW)).toBe("warn");
    expect(backupTone({ status: "ok", created_at: iso(3600_000) }, NOW)).toBe("ok");
    expect(backupTone({ status: "ok", created_at: iso(8 * 86_400_000) }, NOW)).toBe("warn");
    expect(backupTone({ status: "failed", created_at: iso(3600_000) }, NOW)).toBe("fail");
  });
});
