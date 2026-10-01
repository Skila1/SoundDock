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
  seekTo: (ms: number) => void;
};

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

export function updateMediaPosition(s: MediaPosition) {
  try {
    navigator.mediaSession?.setPositionState({
      duration: Math.max(0, (s.duration || 0) / 1000),
      playbackRate: s.playbackRate || 1,
      position: Math.max(0, Math.min((s.duration || 0) / 1000, (s.position || 0) / 1000))
    });
  } catch {
    /* some browsers reject incomplete state */
  }
  if ("mediaSession" in navigator) {
    navigator.mediaSession.playbackState = s.playing ? "playing" : "paused";
  }
}

function setAction(name: MediaSessionAction, handler: MediaSessionActionHandler | null) {
  try {
    navigator.mediaSession?.setActionHandler(name, handler);
  } catch {
    /* unsupported on this engine */
  }
}

export function attachMediaRemote(handlers: MediaRemoteHandlers) {
  setAction("play", () => handlers.play());
  setAction("pause", () => handlers.pause());
  setAction("nexttrack", () => handlers.next());
  setAction("previoustrack", () => handlers.previous());
  setAction("seekto", (e) => {
    if (e.seekTime == null) return;
    handlers.seekTo(e.seekTime * 1000);
  });
  // CarPlay / Android Auto / lock screen show ±10s when these are set.
  setAction("seekforward", null);
  setAction("seekbackward", null);
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
