export type MediaTrackMeta = {
  id: string;
  title: string;
  album?: string;
  artist?: string;
  artists?: { name: string }[];
};

export type MediaPosition = {
  duration: number;
  playbackRate: number;
  position: number;
  playing: boolean;
};

export type MediaRemoteHandlers = {
  play: () => void;
  pause: () => void;
  next: () => void;
  previous: () => void;
  stop: () => void;
  seekTo: (ms: number) => void;
  /** Relative seek in ms (negative to go back). */
  seekBy: (deltaMs: number) => void;
  /** Play/pause toggle for keys that carry no explicit state. */
  toggle: () => void;
};

export type RemoteAction = "play" | "pause" | "toggle" | "next" | "previous" | "stop";
export type RemoteSource = "mediasession" | "keyboard";

const SEEK_STEP_MS = 10_000;
/** A media key can reach us twice: as a Media Session action and as a keydown. */
export const REMOTE_DEDUPE_MS = 400;

type AudioSessionLike = {
  type?: string;
  state?: string;
  addEventListener?: (name: string, fn: () => void) => void;
};

function artworkSrc(id: string, size: string) {
  const path = `/api/v1/tracks/${id}/artwork?size=${size}`;
  if (typeof location === "undefined") return path;
  return new URL(path, location.origin).href;
}

export function mediaArtwork(id: string): MediaImage[] {
  return [
    { src: artworkSrc(id, "thumb"), sizes: "96x96", type: "image/jpeg" },
    { src: artworkSrc(id, "card"), sizes: "300x300", type: "image/jpeg" },
    { src: artworkSrc(id, "now"), sizes: "640x640", type: "image/jpeg" }
  ];
}

export function bindMediaSession(meta: MediaTrackMeta) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: meta.title,
    artist: meta.artists?.map((a) => a.name).join(", ") || meta.artist || "",
    album: meta.album,
    artwork: mediaArtwork(meta.id)
  });
}

export function supportsMediaSession() {
  return typeof navigator !== "undefined" && "mediaSession" in navigator;
}

let lastPosition: (MediaPosition & { at: number }) | null = null;

/** Clears the OS media overlay when nothing is loaded. */
export function clearMediaSession() {
  if (!supportsMediaSession()) return;
  lastPosition = null;
  try {
    navigator.mediaSession.metadata = null;
    navigator.mediaSession.playbackState = "none";
    navigator.mediaSession.setPositionState?.();
  } catch {
    /* older engines */
  }
}

/**
 * Whether a position update is worth sending. Browsers extrapolate the
 * position from the rate, so only state changes or real drift need publishing.
 */
export function positionChanged(prev: (MediaPosition & { at: number }) | null, next: MediaPosition, now: number) {
  if (!prev) return true;
  if (prev.playing !== next.playing || prev.playbackRate !== next.playbackRate) return true;
  if (Math.abs(prev.duration - next.duration) > 500) return true;
  const expected = prev.position + (prev.playing ? (now - prev.at) * (prev.playbackRate || 1) : 0);
  return Math.abs(expected - next.position) > 1000;
}

export function updateMediaPosition(s: MediaPosition) {
  if (!supportsMediaSession()) return;
  const now = Date.now();
  const playbackState = s.playing ? "playing" : "paused";
  if (navigator.mediaSession.playbackState !== playbackState) navigator.mediaSession.playbackState = playbackState;
  if (!positionChanged(lastPosition, s, now)) return;
  lastPosition = { ...s, at: now };
  const duration = Math.max(0, (s.duration || 0) / 1000);
  try {
    navigator.mediaSession.setPositionState?.({
      duration,
      playbackRate: s.playbackRate || 1,
      position: Math.max(0, Math.min(duration, (s.position || 0) / 1000))
    });
  } catch {
    /* some browsers reject incomplete state */
  }
}

function setAction(name: MediaSessionAction, handler: MediaSessionActionHandler | null) {
  try {
    navigator.mediaSession?.setActionHandler(name, handler);
  } catch {
    /* unsupported on this engine */
  }
}

/**
 * iOS and iPadOS replace the lock-screen track buttons with ±seek buttons when
 * seek handlers exist, so keep next/previous there.
 */
export function seekButtonsHideTrackButtons(nav: Pick<Navigator, "userAgent" | "maxTouchPoints"> = navigator) {
  const ua = nav.userAgent || "";
  return /iPad|iPhone|iPod/.test(ua) || (/Macintosh/.test(ua) && (nav.maxTouchPoints || 0) > 1);
}

// Last remote command per group, shared across tabs so a key that one tab sees
// as a keydown and another as a Media Session action only runs once.
const lastRemote = new Map<string, { source: RemoteSource; at: number }>();
let remoteChannel: BroadcastChannel | null | undefined;

function remoteGroup(action: RemoteAction) {
  return action === "play" || action === "pause" || action === "toggle" ? "playpause" : action;
}

function channel() {
  if (remoteChannel !== undefined) return remoteChannel;
  remoteChannel = null;
  if (typeof BroadcastChannel === "undefined") return null;
  try {
    remoteChannel = new BroadcastChannel("sounddock-media-remote");
    remoteChannel.onmessage = (e: MessageEvent<{ group?: string; source?: RemoteSource; at?: number }>) => {
      const d = e.data;
      if (d?.group && d.source && typeof d.at === "number") lastRemote.set(d.group, { source: d.source, at: d.at });
    };
  } catch {
    remoteChannel = null;
  }
  return remoteChannel;
}

/**
 * Records a remote command and reports whether to run it. The same command
 * from the other source within REMOTE_DEDUPE_MS is a duplicate; repeated
 * presses from one source always pass.
 */
export function acceptRemote(action: RemoteAction, source: RemoteSource, now = Date.now()) {
  const group = remoteGroup(action);
  channel();
  const prev = lastRemote.get(group);
  if (prev && prev.source !== source && now - prev.at < REMOTE_DEDUPE_MS) return false;
  lastRemote.set(group, { source, at: now });
  try {
    remoteChannel?.postMessage({ group, source, at: now });
  } catch {
    /* channel closed */
  }
  return true;
}

export function resetRemoteForTests() {
  lastRemote.clear();
  lastPosition = null;
}

const MEDIA_KEYS: Record<string, RemoteAction> = {
  MediaPlayPause: "toggle",
  MediaPlay: "play",
  MediaPause: "pause",
  MediaTrackNext: "next",
  MediaTrackPrevious: "previous",
  MediaStop: "stop"
};

/** Maps a hardware media keydown to a command (foreground fallback). */
export function mediaKeyAction(e: Pick<KeyboardEvent, "key" | "code">): RemoteAction | null {
  return MEDIA_KEYS[e.key] || MEDIA_KEYS[e.code] || null;
}

export function runRemote(action: RemoteAction, source: RemoteSource, handlers: MediaRemoteHandlers) {
  if (!acceptRemote(action, source)) return false;
  handlers[action]();
  return true;
}

export function attachMediaRemote(handlers: MediaRemoteHandlers) {
  if (!supportsMediaSession()) return;
  setAction("play", () => runRemote("play", "mediasession", handlers));
  setAction("pause", () => runRemote("pause", "mediasession", handlers));
  setAction("nexttrack", () => runRemote("next", "mediasession", handlers));
  setAction("previoustrack", () => runRemote("previous", "mediasession", handlers));
  setAction("stop", () => runRemote("stop", "mediasession", handlers));
  setAction("seekto", (e) => {
    if (e.seekTime == null) return;
    handlers.seekTo(e.seekTime * 1000);
  });
  if (seekButtonsHideTrackButtons()) {
    setAction("seekforward", null);
    setAction("seekbackward", null);
  } else {
    setAction("seekforward", (e) => handlers.seekBy(e.seekOffset ? e.seekOffset * 1000 : SEEK_STEP_MS));
    setAction("seekbackward", (e) => handlers.seekBy(-(e.seekOffset ? e.seekOffset * 1000 : SEEK_STEP_MS)));
  }
}

/** Safari / iOS: this is a music session, not a transient sound. */
export function claimPlaybackSession(onResume?: () => void) {
  const session = (navigator as Navigator & { audioSession?: AudioSessionLike }).audioSession;
  if (!session) return;
  try {
    session.type = "playback";
  } catch {
    /* older Safari */
  }
  if (!onResume || !session.addEventListener) return;
  session.addEventListener("statechange", () => {
    if (session.state === "active") onResume();
  });
}
