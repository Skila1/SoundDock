import { create } from "zustand";
import { persist } from "zustand/middleware";
import { api, streamUrl } from "@/lib/api";
import type { QueueState, Track } from "@/types/api";

let audio: HTMLAudioElement | null = null;
let seeking = false;

export function getAudio() {
  if (!audio) {
    audio = new Audio();
    audio.preload = "auto";
  }
  return audio;
}

type PlayerStore = {
  queue: QueueState | null;
  current?: Track;
  playing: boolean;
  volume: number;
  muted: boolean;
  shuffle: boolean;
  repeat: string;
  position: number;
  duration: number;
  load: () => Promise<void>;
  playTracks: (ids: string[], start?: number) => Promise<void>;
  add: (ids: string[], next?: boolean) => Promise<void>;
  control: (action: string, extra?: Record<string, unknown>) => Promise<void>;
  seek: (ms: number) => void;
  setVolume: (v: number) => void;
  toggleMute: () => void;
  hydrateTrack: (id: string) => Promise<void>;
};

async function bindSession(meta: Track) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: meta.title,
    artist: meta.artists?.map((a) => a.name).join(", ") || meta.artist || "",
    album: meta.album,
    artwork: [{ src: `/api/v1/tracks/${meta.id}/artwork?size=card`, sizes: "300x300" }]
  });
}

export const usePlayer = create<PlayerStore>()(
  persist(
    (set, get) => ({
      queue: null,
      playing: false,
      volume: 1,
      muted: false,
      shuffle: false,
      repeat: "off",
      position: 0,
      duration: 0,
      load: async () => {
        try {
          const q = await api.get<QueueState>("/api/v1/me/queue");
          set({
            queue: q,
            playing: q.status === "playing",
            volume: q.volume ?? 1,
            shuffle: !!q.shuffle,
            repeat: q.repeat || "off"
          });
          if (q.current_track_id) await get().hydrateTrack(q.current_track_id);
        } catch {
          /* unauthenticated */
        }
      },
      hydrateTrack: async (id) => {
        try {
          const t = await api.get<Track>(`/api/v1/tracks/${id}`);
          set({ current: t, duration: t.duration_ms || 0 });
          bindSession(t);
        } catch {
          set({ current: { id, title: "Track" } });
        }
      },
      playTracks: async (ids, start = 0) => {
        const q = await api.put<QueueState>("/api/v1/me/queue", { track_ids: ids, start });
        set({ queue: q, playing: true });
        const id = ids[start];
        const a = getAudio();
        a.src = streamUrl(id);
        a.volume = get().muted ? 0 : get().volume;
        await a.play().catch(() => undefined);
        await get().hydrateTrack(id);
      },
      add: async (ids, next) => {
        await api.post("/api/v1/me/queue/add", { track_ids: ids, next });
        await get().load();
      },
      control: async (action, extra) => {
        const q = await api.post<QueueState>("/api/v1/me/queue/control", { action, extra });
        set({ queue: q, playing: q.status === "playing", shuffle: !!q.shuffle, repeat: q.repeat });
        const a = getAudio();
        if (action === "pause") a.pause();
        if (action === "resume") await a.play().catch(() => undefined);
        if (q.current_track_id && q.current_track_id !== get().current?.id) {
          a.src = streamUrl(q.current_track_id);
          if (q.status === "playing") await a.play().catch(() => undefined);
          await get().hydrateTrack(q.current_track_id);
        }
      },
      seek: (ms) => {
        const a = getAudio();
        seeking = true;
        a.currentTime = ms / 1000;
        set({ position: ms });
        seeking = false;
        get().control("seek", { position_ms: ms }).catch(() => undefined);
      },
      setVolume: (v) => {
        getAudio().volume = get().muted ? 0 : v;
        set({ volume: v });
      },
      toggleMute: () => {
        const muted = !get().muted;
        getAudio().volume = muted ? 0 : get().volume;
        set({ muted });
      }
    }),
    { name: "sd-player", partialize: (s) => ({ volume: s.volume, muted: s.muted }) }
  )
);

export function attachAudioListeners() {
  const a = getAudio();
  a.ontimeupdate = () => {
    if (!seeking) usePlayer.setState({ position: a.currentTime * 1000, duration: (a.duration || 0) * 1000 });
  };
  a.onended = () => {
    usePlayer.getState().control("skip");
  };
  a.onplay = () => usePlayer.setState({ playing: true });
  a.onpause = () => usePlayer.setState({ playing: false });
  navigator.mediaSession?.setActionHandler("play", () => usePlayer.getState().control("resume"));
  navigator.mediaSession?.setActionHandler("pause", () => usePlayer.getState().control("pause"));
  navigator.mediaSession?.setActionHandler("nexttrack", () => usePlayer.getState().control("skip"));
  navigator.mediaSession?.setActionHandler("previoustrack", () => usePlayer.getState().control("previous"));
}
