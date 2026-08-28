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
  seekBy: (deltaMs: number) => void;
};

export function bindMediaSession(meta: MediaTrackMeta) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: meta.title,
    artist: meta.artists?.map((a) => a.name).join(", ") || meta.artist || "",
    album: meta.album,
    artwork: [{ src: `/api/v1/tracks/${meta.id}/artwork?size=card`, sizes: "300x300" }]
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

export function attachMediaRemote(handlers: MediaRemoteHandlers) {
  navigator.mediaSession?.setActionHandler("play", () => handlers.play());
  navigator.mediaSession?.setActionHandler("pause", () => handlers.pause());
  navigator.mediaSession?.setActionHandler("nexttrack", () => handlers.next());
  navigator.mediaSession?.setActionHandler("previoustrack", () => handlers.previous());
  try {
    navigator.mediaSession?.setActionHandler("seekto", (e) => {
      if (e.seekTime == null) return;
      handlers.seekTo(e.seekTime * 1000);
    });
  } catch {
    /* unsupported */
  }
  try {
    navigator.mediaSession?.setActionHandler("seekforward", (e) => {
      handlers.seekBy((e.seekOffset ?? 10) * 1000);
    });
    navigator.mediaSession?.setActionHandler("seekbackward", (e) => {
      handlers.seekBy(-((e.seekOffset ?? 10) * 1000));
    });
  } catch {
    /* unsupported */
  }
}
