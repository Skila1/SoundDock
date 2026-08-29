const SKIP_LIST = new Set(["WL", "LL"]);
const PLAYLIST_PREFIXES = ["PL", "OL", "UU", "FL"];

function youtubeHost(raw: string): URL | null {
  try {
    const u = new URL(raw.trim());
    const host = u.hostname.replace(/^www\./i, "").toLowerCase();
    if (host === "youtu.be" || host === "youtube.com" || host === "m.youtube.com" || host === "music.youtube.com" || host.endsWith(".youtube.com")) {
      return u;
    }
  } catch {
    /* not a URL */
  }
  return null;
}

export function youtubePlaylistId(raw: string): string {
  const u = youtubeHost(raw);
  if (!u) return "";
  const id = (u.searchParams.get("list") || "").trim();
  if (!id || SKIP_LIST.has(id.toUpperCase())) return "";
  return id;
}

/** Public playlist page, or a watch URL whose list= is a real playlist (not a Mix). */
export function isYouTubePlaylistQuery(raw: string): boolean {
  const id = youtubePlaylistId(raw);
  if (!id) return false;
  const u = youtubeHost(raw);
  if (!u) return false;
  const path = u.pathname.replace(/\/+$/, "").toLowerCase();
  if (path === "/playlist" || path.endsWith("/playlist")) return true;
  const upper = id.toUpperCase();
  return PLAYLIST_PREFIXES.some((p) => upper.startsWith(p));
}
