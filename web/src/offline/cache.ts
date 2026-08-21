const CACHE_NAME = "sounddock-offline-audio-v1";
const META_KEY = "sounddock.offline.meta";

export type OfflineMeta = {
  trackId: string;
  cachedAt: string;
  bytes?: number;
  expiresAt?: string;
};

function cacheRequest(trackId: string) {
  return new Request(`/__offline/tracks/${trackId}`, { credentials: "same-origin" });
}

export async function openOfflineCache() {
  return caches.open(CACHE_NAME);
}

export async function readMeta(): Promise<Record<string, OfflineMeta>> {
  try {
    return JSON.parse(localStorage.getItem(META_KEY) || "{}");
  } catch {
    return {};
  }
}

function writeMeta(meta: Record<string, OfflineMeta>) {
  localStorage.setItem(META_KEY, JSON.stringify(meta));
}

export async function listOfflineTracks(): Promise<OfflineMeta[]> {
  return Object.values(await readMeta());
}

export async function isOfflineCached(trackId: string): Promise<boolean> {
  const cache = await openOfflineCache();
  return !!(await cache.match(cacheRequest(trackId)));
}

export async function putOfflineTrack(trackId: string, response: Response, expiresAt?: string) {
  const cache = await openOfflineCache();
  const clone = response.clone();
  await cache.put(cacheRequest(trackId), clone);
  const bytes = Number(response.headers.get("content-length") || 0) || undefined;
  const meta = await readMeta();
  meta[trackId] = { trackId, cachedAt: new Date().toISOString(), bytes, expiresAt };
  writeMeta(meta);
}

export async function deleteOfflineTrack(trackId: string) {
  const cache = await openOfflineCache();
  await cache.delete(cacheRequest(trackId));
  const meta = await readMeta();
  delete meta[trackId];
  writeMeta(meta);
}

export async function clearOfflineCache() {
  await caches.delete(CACHE_NAME);
  localStorage.removeItem(META_KEY);
}

export async function offlineObjectUrl(trackId: string): Promise<string | null> {
  const cache = await openOfflineCache();
  const hit = await cache.match(cacheRequest(trackId));
  if (!hit) return null;
  const blob = await hit.blob();
  return URL.createObjectURL(blob);
}
