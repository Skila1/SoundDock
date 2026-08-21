import { create } from "zustand";
import { persist } from "zustand/middleware";
import { api } from "@/lib/api";
import { toast } from "sonner";
import type { QueueState, Track } from "@/types/api";
import {
  discordOptionVisible,
  discordReady,
  loadDevicePrefs,
  resolveOutput,
  saveDevicePrefs,
  setManualOutput,
  type OutputTarget,
  type VoiceState
} from "@/lib/device";
import { useUi } from "@/stores/ui";
import {
  applyRate,
  applyReplayGain,
  applySink,
  applyVolume,
  bindTrack,
  durationMs,
  encoderEndPadSeconds,
  encoderStartSeconds,
  ensureGraph,
  getAudio,
  getIdleAudio,
  looksGapless,
  pauseAll,
  playActive,
  positionMs,
  preloadTrack,
  rampFade,
  remainingMs,
  replayGainMultiplier,
  seekActive,
  setFade,
  swapActive,
  type TrackGainFields
} from "@/components/player/audioEngine";

export type PlayerTrack = Track & TrackGainFields;

export type PlayerQueue = QueueState & {
  shuffle_mode?: string;
  stop_after_current?: boolean;
  device_id?: string | null;
  kind?: string;
};

type ListenScratch = { id: string; counted: boolean; skipped: boolean };

let seeking = false;
let persistPosAt = 0;
let listen: ListenScratch | null = null;
let radioBusy = false;
let xfTimer: number | undefined;
let sleepHandle: number | undefined;
let voiceTimer: number | undefined;
let keysBound = false;
let audioBound = false;
let currentMeta: PlayerTrack | undefined;
let nextMeta: PlayerTrack | undefined;
let skipLocalStart = false;
let advancing = false;

function emptyQueue(partial?: Partial<PlayerQueue>): PlayerQueue {
  return {
    id: "",
    status: "stopped",
    volume: 1,
    shuffle: false,
    repeat: "off",
    crossfade_seconds: 0,
    replaygain_mode: "off",
    current_index: 0,
    current_track_id: null,
    position_ms: 0,
    items: [],
    ...partial
  };
}

function idsOf(q: PlayerQueue | null | undefined) {
  return q?.items?.map((i) => i.track_id) || [];
}

function typingTarget(el: EventTarget | null) {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (el.isContentEditable) return true;
  return !!el.closest("[contenteditable='true'], input, textarea, select");
}

async function fetchVoice(): Promise<VoiceState | null> {
  try {
    const r = await fetch("/api/v1/me/discord/voice-state", { credentials: "include" });
    if (!r.ok) return null;
    return (await r.json()) as VoiceState;
  } catch {
    return null;
  }
}

async function postListen(trackId: string, position: number, duration: number, event: "progress" | "skip") {
  try {
    await api.post("/api/v1/me/listen", {
      track_id: trackId,
      position_ms: Math.round(position),
      duration_ms: Math.round(duration),
      source: "web",
      event
    });
  } catch {
    /* listen is best-effort */
  }
}

function beginListen(id: string) {
  if (listen?.id === id) return;
  listen = { id, counted: false, skipped: false };
}

function markProgress(id: string, pos: number, dur: number) {
  if (!listen || listen.id !== id || listen.counted) return;
  const threshold = Math.min(30000, (dur || 0) * 0.5);
  if (dur > 0 && pos >= threshold) {
    listen.counted = true;
    postListen(id, pos, dur, "progress");
  }
}

function markSkip(id: string, pos: number, dur: number, stopAfter: boolean) {
  if (stopAfter) return;
  if (!listen || listen.id !== id || listen.counted || listen.skipped) return;
  listen.skipped = true;
  postListen(id, pos, dur, "skip");
}

async function bindSession(meta: Track) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: meta.title,
    artist: meta.artists?.map((a) => a.name).join(", ") || meta.artist || "",
    album: meta.album,
    artwork: [{ src: `/api/v1/tracks/${meta.id}/artwork?size=card`, sizes: "300x300" }]
  });
  updatePositionState();
}

function updatePositionState() {
  const s = usePlayer.getState();
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

async function joinDiscord() {
  return api.post<{ ok?: boolean; guild_id?: string; channel_id?: string }>("/api/v1/me/discord/join");
}

async function playDiscord(trackIds: string[], start: number) {
  return api.post("/api/v1/me/discord/play", { track_ids: trackIds, start });
}

type PlayerStore = {
  queue: PlayerQueue | null;
  current?: PlayerTrack;
  playing: boolean;
  volume: number;
  muted: boolean;
  shuffle: boolean;
  repeat: string;
  position: number;
  duration: number;
  playbackRate: number;
  autoplay: boolean;
  visualizer: boolean;
  tinyMode: boolean;
  stopAfterCurrent: boolean;
  sleepUntil: number | null;
  output: OutputTarget;
  voice: VoiceState | null;
  sinkId: string;
  load: () => Promise<void>;
  playTracks: (ids: string[], start?: number) => Promise<void>;
  playNow: (index: number) => Promise<void>;
  add: (ids: string[], next?: boolean) => Promise<void>;
  control: (action: string, extra?: Record<string, unknown>) => Promise<void>;
  seek: (ms: number) => void;
  setVolume: (v: number) => void;
  toggleMute: () => void;
  hydrateTrack: (id: string) => Promise<PlayerTrack | undefined>;
  setOutput: (o: OutputTarget) => Promise<void>;
  pollVoice: () => Promise<void>;
  setPlaybackRate: (r: number) => void;
  setAutoplay: (on: boolean) => void;
  setVisualizer: (on: boolean) => void;
  setTinyMode: (on: boolean) => void;
  setStopAfterCurrent: (on: boolean) => Promise<void>;
  setSleep: (minutes: number | null) => void;
  setSink: (id: string) => Promise<void>;
  saveQueueAsPlaylist: (name: string) => Promise<void>;
};

function applyQueueFields(q: PlayerQueue) {
  return {
    queue: q,
    playing: q.status === "playing",
    volume: q.volume ?? usePlayer.getState().volume,
    shuffle: !!q.shuffle,
    repeat: q.repeat || "off",
    stopAfterCurrent: !!q.stop_after_current,
    position: q.position_ms || 0
  };
}

function usingDiscord() {
  const s = usePlayer.getState();
  if (!discordOptionVisible(s.voice)) return false;
  return resolveOutput(s.voice, loadDevicePrefs().outputManual) === "discord";
}

function discordBlocked() {
  const s = usePlayer.getState();
  return usingDiscord() && !discordReady(s.voice);
}

async function startLocal(id: string, positionMsValue: number, shouldPlay: boolean, meta?: PlayerTrack) {
  const a = getAudio();
  await bindTrack(a, id);
  applyReplayGain(replayGainMultiplier(usePlayer.getState().queue?.replaygain_mode, meta || currentMeta));
  applyRate(usePlayer.getState().playbackRate);
  applyVolume(usePlayer.getState().volume, usePlayer.getState().muted);
  const startPad = encoderStartSeconds(meta || currentMeta);
  const applyPos = () => {
    const pos = Math.max(startPad, (positionMsValue || 0) / 1000);
    try {
      a.currentTime = pos;
    } catch {
      /* wait for metadata */
    }
  };
  if (a.readyState >= 1) applyPos();
  else a.onloadedmetadata = () => applyPos();
  if (!shouldPlay) {
    try {
      a.pause();
    } catch {
      /* ignore */
    }
    return false;
  }
  try {
    await playActive();
    return true;
  } catch {
    return false;
  }
}

async function maybeReplenishRadio() {
  const s = usePlayer.getState();
  if (!s.autoplay || s.stopAfterCurrent || radioBusy) return;
  const items = s.queue?.items || [];
  const idx = s.queue?.current_index ?? 0;
  if (!items.length || items.length - idx > 2) return;
  const seed = s.current?.id;
  if (!seed) return;
  radioBusy = true;
  try {
    const r = await api.get<{ track_ids?: string[] }>(`/api/v1/radio?kind=track&seed_id=${encodeURIComponent(seed)}&limit=20`);
    const extra = (r.track_ids || []).filter((id) => id && id !== seed && !items.some((it) => it.track_id === id));
    if (extra.length) await s.add(extra, false);
  } catch {
    /* radio optional */
  } finally {
    radioBusy = false;
  }
}

function scheduleCrossfade() {
  if (xfTimer) window.clearTimeout(xfTimer);
  const s = usePlayer.getState();
  if (usingDiscord()) return;
  const xf = (s.queue?.crossfade_seconds || 0) * 1000;
  const gapless = looksGapless(currentMeta, nextMeta);
  const pad = encoderEndPadSeconds(currentMeta) * 1000;
  const remain = remainingMs();
  if (!Number.isFinite(remain)) return;
  const lead = gapless ? Math.max(40, pad) : xf > 0 ? xf : 0;
  if (lead <= 0) return;
  const wait = Math.max(0, remain - lead);
  xfTimer = window.setTimeout(() => {
    beginNext(true, gapless, xf);
  }, wait);
}

async function beginNext(fromEnded: boolean, gapless: boolean, xfMs: number) {
  if (advancing) return;
  advancing = true;
  try {
  const s = usePlayer.getState();
  if (s.stopAfterCurrent) {
    await s.control("stop");
    return;
  }
  if (s.repeat === "one" && fromEnded) {
    listen = null;
    const id = s.current?.id;
    if (id) {
      beginListen(id);
      seekActive(encoderStartSeconds(currentMeta) * 1000);
      try {
        await playActive();
      } catch {
        /* ignore */
      }
    }
    return;
  }
  const items = s.queue?.items || [];
  const idx = s.queue?.current_index ?? 0;
  let next = idx + 1;
  if (next >= items.length) {
    if (s.repeat === "queue") next = 0;
    else {
      await s.control("stop");
      return;
    }
  }
  const idle = getIdleAudio();
  const nextId = items[next]?.track_id;
  if (nextId && idle && idle.dataset.trackId === nextId && !usingDiscord()) {
    const useXf = !gapless && xfMs > 0;
    if (useXf) {
      const idleSlot = idle === getAudio() ? 0 : 1;
      setFade(idleSlot, 0);
      try {
        idle.currentTime = encoderStartSeconds(nextMeta);
        await idle.play();
      } catch {
        await s.control("skip");
        return;
      }
      const from = idleSlot;
      const to = from === 0 ? 1 : 0;
      rampFade(to, 1, 0, xfMs);
      rampFade(from, 0, 1, xfMs);
      window.setTimeout(() => {
        getAudio().pause();
        swapActive();
        setFade(0, 1);
        setFade(1, 1);
      }, xfMs);
      skipLocalStart = true;
      await s.control("skip");
      skipLocalStart = false;
      return;
    }
    try {
      idle.currentTime = encoderStartSeconds(nextMeta);
      await idle.play();
      getAudio().pause();
      swapActive();
      skipLocalStart = true;
      await s.control("skip");
      skipLocalStart = false;
      return;
    } catch {
      /* control skip below */
    }
  }
  await s.control("skip");
  } finally {
    advancing = false;
  }
}

const prefs = loadDevicePrefs();

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
      playbackRate: prefs.playbackRate,
      autoplay: prefs.autoplay,
      visualizer: prefs.visualizer,
      tinyMode: prefs.tinyMode,
      stopAfterCurrent: false,
      sleepUntil: null,
      output: resolveOutput(null, prefs.outputManual),
      voice: null,
      sinkId: prefs.sinkId,
      load: async () => {
        try {
          const q = (await api.get<PlayerQueue>("/api/v1/me/queue")) || emptyQueue();
          set({
            ...applyQueueFields(q),
            playing: false
          });
          applyVolume(get().volume, get().muted);
          if (q.current_track_id) {
            const t = await get().hydrateTrack(q.current_track_id);
            const discord = usingDiscord();
            const wantPlay = q.status === "playing" && !discord;
            const played = await startLocal(q.current_track_id, q.position_ms || 0, wantPlay, t);
            set({ playing: wantPlay && played, position: q.position_ms || 0, duration: t?.duration_ms || durationMs() });
            if (played) beginListen(q.current_track_id);
          }
          const nextId = q.items?.[(q.current_index ?? 0) + 1]?.track_id;
          if (nextId) {
            preloadTrack(nextId);
            get()
              .hydrateTrack(nextId)
              .then((t) => {
                nextMeta = t;
              })
              .catch(() => undefined);
          }
        } catch {
          /* unauthenticated */
        }
      },
      hydrateTrack: async (id) => {
        try {
          const t = await api.get<PlayerTrack>(`/api/v1/tracks/${id}`);
          if (get().queue?.current_track_id === id || get().current?.id === id || !get().current) {
            currentMeta = t;
            set({ current: t, duration: t.duration_ms || durationMs() || 0 });
            bindSession(t);
            applyReplayGain(replayGainMultiplier(get().queue?.replaygain_mode, t));
          }
          return t;
        } catch {
          const fallback: PlayerTrack = { id, title: "Track" };
          if (!get().current || get().current?.id === id) set({ current: fallback });
          return fallback;
        }
      },
      playTracks: async (ids, start = 0) => {
        if (!ids.length) return;
        const idx = Math.max(0, Math.min(start, ids.length - 1));
        await get().pollVoice();
        if (discordBlocked()) {
          toast.error("Join a Discord voice channel to play");
          return;
        }
        if (usingDiscord()) {
          pauseAll();
          try {
            await joinDiscord();
            await playDiscord(ids, idx);
          } catch (e) {
            const msg = e instanceof Error ? e.message : "Discord play failed";
            toast.error(msg);
            return;
          }
          const q = emptyQueue({
            items: ids.map((track_id, position) => ({ id: track_id, position, track_id })),
            current_index: idx,
            current_track_id: ids[idx],
            status: "playing"
          });
          set({ queue: q, playing: true, position: 0 });
          await get().hydrateTrack(ids[idx]);
          return;
        }
        const q = await api.put<PlayerQueue>("/api/v1/me/queue", { track_ids: ids, start: idx });
        set({ ...applyQueueFields(q), playing: true });
        const id = ids[idx];
        currentMeta = undefined;
        listen = null;
        const t = await get().hydrateTrack(id);
        const played = await startLocal(id, 0, true, t);
        set({ playing: played });
        if (played) beginListen(id);
        const nid = ids[idx + 1];
        if (nid) preloadTrack(nid);
      },
      playNow: async (index) => {
        const q = get().queue;
        const ids = idsOf(q);
        if (!q || index < 0 || index >= ids.length) return;
        await get().pollVoice();
        if (discordBlocked()) {
          toast.error("Join a Discord voice channel to play");
          return;
        }
        if (usingDiscord()) {
          pauseAll();
          try {
            await joinDiscord();
            await playDiscord(ids, index);
          } catch (e) {
            toast.error(e instanceof Error ? e.message : "Discord play failed");
            return;
          }
          set({
            queue: { ...q, current_index: index, current_track_id: ids[index], status: "playing" },
            playing: true,
            position: 0
          });
          await get().hydrateTrack(ids[index]);
          return;
        }
        listen = null;
        let jumped = false;
        try {
          await get().control("index", { index });
          jumped = get().queue?.current_track_id === ids[index];
        } catch {
          /* P1 may not have index yet — still jump locally, never PUT-replace */
        }
        if (jumped) return;
        const latest = get().queue;
        if (!latest?.items[index]) return;
        set({
          queue: { ...latest, current_index: index, current_track_id: ids[index], status: "playing" },
          playing: true,
          position: 0
        });
        const t = await get().hydrateTrack(ids[index]);
        currentMeta = t;
        const played = await startLocal(ids[index], 0, true, t);
        set({ playing: played });
        if (played) beginListen(ids[index]);
        const nid = ids[index + 1];
        if (nid) preloadTrack(nid);
      },
      add: async (ids, next) => {
        const existing = new Set(idsOf(get().queue));
        const dups = ids.filter((id) => existing.has(id));
        if (dups.length) toast.warning(dups.length === 1 ? "That track is already in the queue" : `${dups.length} tracks are already in the queue`);
        await api.post("/api/v1/me/queue/add", { track_ids: ids, next });
        try {
          const q = await api.get<PlayerQueue>("/api/v1/me/queue");
          set({
            queue: q,
            shuffle: !!q.shuffle,
            repeat: q.repeat || get().repeat,
            stopAfterCurrent: !!q.stop_after_current
          });
        } catch {
          /* keep local queue */
        }
      },
      control: async (action, extra) => {
        const before = get();
        if ((action === "skip" || action === "next" || action === "previous") && before.current) {
          markSkip(before.current.id, before.position, before.duration, before.stopAfterCurrent);
        }
        if (action === "pause") pauseAll();
        let q: PlayerQueue | null = null;
        try {
          q = await api.post<PlayerQueue>("/api/v1/me/queue/control", { action, extra });
        } catch (e) {
          if (action === "index") throw e;
          if (action === "stop_after_current" || action === "reorder" || action === "seek") return;
          toast.error(e instanceof Error ? e.message : "Playback control failed");
          return;
        }
        if (!q) return;
        const prevId = get().current?.id;
        const metaOnly = action === "seek" || action === "reorder" || action === "volume" || action === "stop_after_current";
        set({
          queue: q,
          shuffle: !!q.shuffle,
          repeat: q.repeat || get().repeat,
          stopAfterCurrent: q.stop_after_current ?? get().stopAfterCurrent,
          volume: q.volume ?? get().volume,
          ...(metaOnly ? {} : { playing: q.status === "playing" })
        });
        const discord = usingDiscord();
        if (!metaOnly && (action === "pause" || q.status === "paused" || q.status === "stopped")) {
          pauseAll();
          set({ playing: false });
        }
        if (action === "resume" && !discord) {
          try {
            await playActive();
            set({ playing: true });
            if (get().current?.id) beginListen(get().current!.id);
          } catch {
            set({ playing: false });
          }
        }
        if (action === "resume" && discord) {
          const ids = idsOf(q);
          if (ids.length && discordReady(get().voice)) {
            try {
              await joinDiscord();
              await playDiscord(ids, q.current_index || 0);
              set({ playing: true });
            } catch (err) {
              toast.error(err instanceof Error ? err.message : "Discord play failed");
            }
          }
        }
        if (action === "stop" || action === "clear") {
          pauseAll();
          set({ playing: false, position: 0 });
        }
        if (q.current_track_id && q.current_track_id !== prevId) {
          const t = await get().hydrateTrack(q.current_track_id);
          currentMeta = t;
          if (skipLocalStart) {
            applyReplayGain(replayGainMultiplier(get().queue?.replaygain_mode, t));
          } else if (!discord && q.status === "playing") {
            const played = await startLocal(q.current_track_id, q.position_ms || 0, true, t);
            set({ playing: played });
            if (played) {
              listen = null;
              beginListen(q.current_track_id);
            }
          } else if (!discord) {
            await startLocal(q.current_track_id, q.position_ms || 0, false, t);
          } else {
            pauseAll();
          }
          const nid = q.items?.[(q.current_index ?? 0) + 1]?.track_id;
          if (nid) {
            preloadTrack(nid);
            get()
              .hydrateTrack(nid)
              .then((m) => {
                nextMeta = m;
              })
              .catch(() => undefined);
          }
        }
      },
      seek: (ms) => {
        seeking = true;
        seekActive(ms);
        set({ position: ms });
        seeking = false;
        const t = get().current;
        if (t) markProgress(t.id, ms, get().duration);
        get().control("seek", { position_ms: Math.round(ms) }).catch(() => undefined);
        updatePositionState();
      },
      setVolume: (v) => {
        const next = Math.min(1, Math.max(0, v));
        const muted = next <= 0;
        applyVolume(next, muted);
        set({ volume: next, muted });
      },
      toggleMute: () => {
        const muted = !get().muted;
        applyVolume(get().volume, muted);
        set({ muted });
      },
      setOutput: async (o) => {
        setManualOutput(o);
        set({ output: o });
        if (o === "discord") {
          pauseAll();
          if (get().playing || get().queue?.status === "playing") {
            if (!discordReady(get().voice)) {
              toast.error("Join a Discord voice channel to play");
              set({ playing: false });
              return;
            }
            const ids = idsOf(get().queue);
            try {
              await joinDiscord();
              await playDiscord(ids, get().queue?.current_index || 0);
              set({ playing: true });
            } catch (e) {
              toast.error(e instanceof Error ? e.message : "Discord play failed");
            }
          }
          return;
        }
        const id = get().current?.id;
        if (id && (get().playing || get().queue?.status === "playing")) {
          const played = await startLocal(id, get().position, true, get().current);
          set({ playing: played });
        }
      },
      pollVoice: async () => {
        const voice = await fetchVoice();
        const manual = loadDevicePrefs().outputManual;
        const output = resolveOutput(voice, manual);
        set({ voice, output });
      },
      setPlaybackRate: (r) => {
        const rate = Math.min(2, Math.max(0.5, r));
        applyRate(rate);
        saveDevicePrefs({ playbackRate: rate });
        set({ playbackRate: rate });
        updatePositionState();
      },
      setAutoplay: (on) => {
        saveDevicePrefs({ autoplay: on });
        set({ autoplay: on });
        if (on) maybeReplenishRadio();
      },
      setVisualizer: (on) => {
        saveDevicePrefs({ visualizer: on });
        set({ visualizer: on });
        if (on) ensureGraph(getAudio());
      },
      setTinyMode: (on) => {
        saveDevicePrefs({ tinyMode: on });
        set({ tinyMode: on });
      },
      setStopAfterCurrent: async (on) => {
        set({ stopAfterCurrent: on });
        try {
          await get().control("stop_after_current", { enabled: on });
        } catch {
          /* P1 additive */
        }
      },
      setSleep: (minutes) => {
        if (sleepHandle) window.clearTimeout(sleepHandle);
        sleepHandle = undefined;
        if (minutes == null || minutes < 0) {
          set({ sleepUntil: null });
          return;
        }
        if (minutes === 0) {
          get().setStopAfterCurrent(true);
          set({ sleepUntil: null });
          return;
        }
        const until = Date.now() + minutes * 60_000;
        set({ sleepUntil: until });
        sleepHandle = window.setTimeout(() => {
          get().control("pause");
          set({ sleepUntil: null });
        }, minutes * 60_000);
      },
      setSink: async (id) => {
        saveDevicePrefs({ sinkId: id });
        set({ sinkId: id });
        await applySink(id);
      },
      saveQueueAsPlaylist: async (name) => {
        const ids = idsOf(get().queue);
        if (!ids.length) return;
        const created = await api.post<{ id: string }>("/api/v1/playlists", { name: name || "Queue" });
        if (created?.id) await api.post(`/api/v1/playlists/${created.id}/tracks`, { track_ids: ids });
        toast.success("Saved as playlist");
      }
    }),
    { name: "sd-player", partialize: (s) => ({ volume: s.volume, muted: s.muted }) }
  )
);

function onTimeUpdate(el: HTMLAudioElement) {
  if (el !== getAudio() || seeking) return;
  const pos = positionMs();
  const dur = durationMs() || usePlayer.getState().duration;
  usePlayer.setState({ position: pos, duration: dur || usePlayer.getState().duration });
  const cur = usePlayer.getState().current;
  if (cur) markProgress(cur.id, pos, dur);
  if (Date.now() - persistPosAt > 10000 && usePlayer.getState().playing) {
    persistPosAt = Date.now();
    usePlayer.getState().control("seek", { position_ms: Math.round(pos) }).catch(() => undefined);
  }
  if (remainingMs() < 12000) maybeReplenishRadio();
  scheduleCrossfade();
  updatePositionState();
}

function onEnded(el: HTMLAudioElement) {
  if (el !== getAudio()) return;
  const s = usePlayer.getState();
  if (s.stopAfterCurrent) {
    s.control("stop");
    return;
  }
  if (s.repeat === "one") {
    listen = null;
    beginListen(s.current?.id || "");
    seekActive(encoderStartSeconds(currentMeta) * 1000);
    playActive().catch(() => undefined);
    return;
  }
  beginNext(true, looksGapless(currentMeta, nextMeta), (s.queue?.crossfade_seconds || 0) * 1000);
}

export function attachAudioListeners() {
  if (!audioBound) {
    audioBound = true;
    const bind = (el: HTMLAudioElement) => {
      el.ontimeupdate = () => onTimeUpdate(el);
      el.onended = () => onEnded(el);
      el.onplay = () => {
        if (el === getAudio()) usePlayer.setState({ playing: true });
      };
      el.onpause = () => {
        if (el === getAudio() && getIdleAudio()?.paused !== false) usePlayer.setState({ playing: false });
      };
      el.onloadedmetadata = () => {
        if (el === getAudio()) usePlayer.setState({ duration: durationMs() || usePlayer.getState().duration });
      };
    };
    bind(getAudio());
    const idle = getIdleAudio();
    if (idle) bind(idle);
    applyRate(usePlayer.getState().playbackRate);
    applyVolume(usePlayer.getState().volume, usePlayer.getState().muted);
    const sink = loadDevicePrefs().sinkId;
    if (sink) applySink(sink);
  }

  navigator.mediaSession?.setActionHandler("play", () => usePlayer.getState().control("resume"));
  navigator.mediaSession?.setActionHandler("pause", () => usePlayer.getState().control("pause"));
  navigator.mediaSession?.setActionHandler("nexttrack", () => usePlayer.getState().control("skip"));
  navigator.mediaSession?.setActionHandler("previoustrack", () => usePlayer.getState().control("previous"));
  try {
    navigator.mediaSession?.setActionHandler("seekto", (e) => {
      if (e.seekTime == null) return;
      usePlayer.getState().seek(e.seekTime * 1000);
    });
  } catch {
    /* unsupported */
  }
  try {
    navigator.mediaSession?.setActionHandler("seekforward", (e) => {
      const off = (e.seekOffset ?? 10) * 1000;
      usePlayer.getState().seek(usePlayer.getState().position + off);
    });
    navigator.mediaSession?.setActionHandler("seekbackward", (e) => {
      const off = (e.seekOffset ?? 10) * 1000;
      usePlayer.getState().seek(Math.max(0, usePlayer.getState().position - off));
    });
  } catch {
    /* unsupported */
  }

  if (!keysBound) {
    keysBound = true;
    window.addEventListener("keydown", (e) => {
      if (useUi.getState().commandOpen) return;
      if (typingTarget(e.target)) return;
      const p = usePlayer.getState();
      if (e.code === "Space") {
        e.preventDefault();
        p.control(p.playing ? "pause" : "resume");
      } else if (e.code === "ArrowRight") {
        e.preventDefault();
        p.seek(p.position + 5000);
      } else if (e.code === "ArrowLeft") {
        e.preventDefault();
        p.seek(Math.max(0, p.position - 5000));
      } else if (e.code === "ArrowUp") {
        e.preventDefault();
        p.setVolume(p.volume + 0.05);
      } else if (e.code === "ArrowDown") {
        e.preventDefault();
        p.setVolume(p.volume - 0.05);
      } else if (e.key === "n" || e.key === "N") {
        p.control("skip");
      } else if (e.key === "p" || e.key === "P") {
        p.control("previous");
      } else if (e.key === "m" || e.key === "M") {
        p.toggleMute();
      }
    });
  }

  if (!voiceTimer) {
    voiceTimer = window.setInterval(() => usePlayer.getState().pollVoice(), 4000);
    usePlayer.getState().pollVoice();
  }
}

export { getAudio, getAnalyser } from "@/components/player/audioEngine";
