import { mintOfflineToken } from "./tokens";
import { isOfflineCached, putOfflineTrack } from "./cache";

export const MAX_CONCURRENT_FILLS = 2;

export type FillStatus = "queued" | "fetching" | "cached" | "error" | "skipped";

export type FillProgress = {
  trackId: string;
  status: FillStatus;
  error?: string;
  active: number;
  remaining: number;
};

type Job = {
  trackId: string;
  run: () => Promise<void>;
};

const queue: Job[] = [];
let active = 0;

function pump() {
  while (active < MAX_CONCURRENT_FILLS && queue.length) {
    const job = queue.shift()!;
    active++;
    job.run().finally(() => {
      active--;
      pump();
    });
  }
}

export function fillTracks(trackIds: string[], onProgress?: (p: FillProgress) => void): Promise<void> {
  const unique = [...new Set(trackIds.filter(Boolean))];
  if (!unique.length) return Promise.resolve();

  let remaining = unique.length;
  const notify = (trackId: string, status: FillStatus, error?: string) => {
    onProgress?.({ trackId, status, error, active, remaining });
  };

  return new Promise((resolve) => {
    let settled = 0;
    const done = () => {
      settled++;
      if (settled >= unique.length) resolve();
    };

    for (const trackId of unique) {
      notify(trackId, "queued");
      queue.push({
        trackId,
        run: async () => {
          try {
            if (await isOfflineCached(trackId)) {
              remaining--;
              notify(trackId, "skipped");
              return;
            }
            notify(trackId, "fetching");
            const minted = await mintOfflineToken(trackId);
            const res = await fetch(minted.url, { credentials: "include", cache: "no-store" });
            if (!res.ok) throw new Error(res.status === 429 ? "stream limit" : res.statusText);
            await putOfflineTrack(trackId, res, minted.expires_at);
            remaining--;
            notify(trackId, "cached");
          } catch (e) {
            remaining--;
            notify(trackId, "error", e instanceof Error ? e.message : "fill failed");
          } finally {
            done();
          }
        }
      });
    }
    pump();
  });
}
