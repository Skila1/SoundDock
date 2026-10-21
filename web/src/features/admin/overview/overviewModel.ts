/** Pure helpers behind the admin Overview (Dashboard and Activity). */

export type PillTone = "ok" | "warn" | "fail";

/** Where each problem is fixed. Kept in one place so the targets move together. */
export const fixLinks = {
  diagnostics: "/admin/updates",
  updates: "/admin/updates",
  discordBot: "/admin/discord",
  librarySettings: "/admin/media-settings",
  library: "/admin/libraries",
  storage: "/admin/storage",
  backups: "/admin/backups",
  activity: "/admin/activity",
  failedJobs: "/admin/activity?jobs=failed",
  downloadErrors: "/admin/activity?tab=logs&show=errors&source=acquisition"
} as const;

export type ErrorItem = { at?: string; created_at?: string; message?: string; last_error?: string; error?: string; status?: string };

const DAY_MS = 86_400_000;

/** Errors from the last 24 hours. */
export function recentErrors<T extends ErrorItem>(items: readonly T[], now: number = Date.now(), windowMs = DAY_MS): T[] {
  return items.filter((e) => {
    const t = Date.parse(e.at || e.created_at || "");
    return Number.isFinite(t) && now - t <= windowMs;
  });
}

const blockedPattern = /sign in to confirm|not a bot|http error 403|403 forbidden|status 403|\b403\b/i;

/** True when a download error looks like YouTube refusing the request (403 or bot check). */
export function isYouTubeBlock(e: ErrorItem): boolean {
  return blockedPattern.test(`${e.message || ""} ${e.last_error || ""} ${e.error || ""}`);
}

export type Attention = { id: string; tone: Exclude<PillTone, "ok">; title: string; detail: string; action: string; to: string };

export type AttentionInput = {
  failedJobs24h: number;
  failedDownloads24h: number;
  youtubeBlocked24h: number;
  passphraseMissing: boolean;
  statsCutoverPending: boolean;
};

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/** The "Needs attention" list, most urgent first. Empty means all clear. */
export function attentionItems(i: AttentionInput): Attention[] {
  const out: Attention[] = [];
  if (i.youtubeBlocked24h > 0) {
    out.push({
      id: "youtube",
      tone: "fail",
      title: "YouTube is blocking downloads",
      detail: `${plural(i.youtubeBlocked24h, "download")} hit a 403 or bot check today. Refresh the YouTube cookies file on the host (SCAPEX_YT_COOKIES).`,
      action: "See errors",
      to: fixLinks.downloadErrors
    });
  }
  if (i.failedJobs24h > 0) {
    out.push({
      id: "jobs",
      tone: "fail",
      title: `${plural(i.failedJobs24h, "job")} failed today`,
      detail: "Retry them or read the error on each one.",
      action: "Review jobs",
      to: fixLinks.failedJobs
    });
  }
  const otherDownloads = Math.max(0, i.failedDownloads24h - i.youtubeBlocked24h);
  if (otherDownloads > 0) {
    out.push({
      id: "downloads",
      tone: "warn",
      title: `${plural(otherDownloads, "download")} failed today`,
      detail: "Songs people asked for could not be fetched.",
      action: "See errors",
      to: fixLinks.downloadErrors
    });
  }
  if (i.passphraseMissing) {
    out.push({
      id: "passphrase",
      tone: "warn",
      title: "Backup passphrase not set",
      detail: "Without it, backups cannot restore your encrypted secrets.",
      action: "Set passphrase",
      to: fixLinks.backups
    });
  }
  if (i.statsCutoverPending) {
    out.push({
      id: "stats",
      tone: "warn",
      title: "Listening stats migration pending",
      detail: "Stats still read the old history table. Run the one-time switch-over.",
      action: "Open migration",
      to: fixLinks.diagnostics
    });
  }
  return out;
}

/** Sum of used bytes across storage backends, or null when none report it. */
export function storageUsed(rows: readonly { used_bytes?: number; total_bytes?: number }[]): { used: number; total: number } | null {
  const withUse = rows.filter((r) => typeof r.used_bytes === "number");
  if (!withUse.length) return null;
  return {
    used: withUse.reduce((n, r) => n + (r.used_bytes || 0), 0),
    total: withUse.reduce((n, r) => n + (r.total_bytes || 0), 0)
  };
}

/** Last backup tone: stale after a week, failed when its status says so. */
export function backupTone(last: { status?: string; created_at?: string } | undefined, now: number = Date.now()): PillTone {
  if (!last?.created_at) return "warn";
  if (last.status && /fail|error/i.test(last.status)) return "fail";
  const age = now - Date.parse(last.created_at);
  return Number.isFinite(age) && age > 7 * DAY_MS ? "warn" : "ok";
}
