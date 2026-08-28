import { fillTracks } from "@/offline";
import { toast } from "sonner";

export function fillableTrackIds(items: { track_id?: string; id?: string; media_state?: string }[]): string[] {
  return [...new Set(items
    .filter((i) => !i.media_state || i.media_state === "ready")
    .map((i) => i.track_id || i.id)
    .filter((id): id is string => !!id))];
}

export async function saveTracksOffline(trackIds: string[]) {
  const ids = [...new Set(trackIds.filter(Boolean))];
  if (!ids.length) {
    toast.message("Nothing ready to save offline");
    return;
  }
  toast.message("Saving for offline playback…");
  let cached = 0;
  let skipped = 0;
  let failed = 0;
  await fillTracks(ids, (p) => {
    if (p.status === "cached") cached++;
    if (p.status === "skipped") skipped++;
    if (p.status === "error") failed++;
  });
  const ok = cached + skipped;
  if (failed && !ok) toast.error("Could not save tracks offline");
  else if (failed) toast.error(`Saved ${ok} offline, ${failed} failed`);
  else toast.success(ok ? `Saved ${ok} tracks offline` : "Already available offline");
}
