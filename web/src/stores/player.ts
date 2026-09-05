import { create } from "zustand";
import { persist } from "zustand/middleware";
import { queryClient } from "@/app/providers";
import { api } from "@/lib/api";
import { isLibraryTrackId } from "@/lib/utils";
import { toast } from "sonner";
import type { MediaState, QueueState, Track } from "@/types/api";
import {
  askRendererTabsToStop,
  discordReady,
  getDeviceId,
  getTabRendererId,
  loadDevicePrefs,
  saveDevicePrefs,
  setManualOutput,
  subscribeRendererChannel,
  type OutputTarget,
  type VoiceState
} from "@/lib/device";
import { useUi } from "@/stores/ui";
import { usePrefs } from "@/stores/prefs";
import { createCommandClient, newCommandId } from "@/stores/commandClient";
import { attachMediaRemote, bindMediaSession, updateMediaPosition } from "@/stores/mediaSession";
import { interpolatePosition, parseTimeMs, sampleClock, type ClockSample } from "@/stores/playhead";
import {
  applyBindResult,
  applyJoinFailure,
  applySnapshot,
  applySwitchToBrowser,
  initialSession,
  shouldStopHtmlAudio,
  tabOwnsBrowserLease,
  type QueueSnapshot,
  type SessionView
} from "@/stores/sessionReducer";
import {
  createQueueSseClient,
  mergePresence,
  pickListeners,
  playheadEventToSnap,
  type PresenceParticipant,
  type QueueSseClient
} from "@/stores/sseClient";
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
  isMediaReady,
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
  stopElement,
  swapActive,
  type TrackGainFields
} from "@/components/player/audioEngine";

export type PlayerTrack = Track & TrackGainFields;

/** Queue item extras from GET/SSE (W6-http). Keep local so we do not race api.ts. */
export type RequestedBy = {
  user_id?: string;
  discord_user_id?: string;
  display_name?: string;
};

export type QueueTrackHint = {
  id: string;
  title?: string;
  artist?: string;
  duration_ms?: number;
};

export type PlayerQueueItem = {
  id: string;
  position: number;
  track_id: string;
  origin?: string;
  media_state?: MediaState;
  intent_id?: string;
  youtube_id?: string;
  external_id?: string;
  title?: string;
  artist?: string;
  duration_ms?: number;
  requested_by?: RequestedBy;
};

export type PlayerQueue = Omit<QueueState, "items"> & {
  items: PlayerQueueItem[];
  shuffle_mode?: string;
  stop_after_current?: boolean;
  device_id?: string | null;
  kind?: string;
};

/** Last remove/clear snapshot; valid only while state_revision === undo_generation. */
export type PendingUndo = {
  undo_generation: number;
  items: PlayerQueueItem[];
};

type ListenScratch = { id: string; counted: boolean; skipped: boolean };

let seeking = false;
let persistPosAt = 0;
let listen: ListenScratch | null = null;
let radioBusy = false;
let xfTimer: number | undefined;
let sleepHandle: number | undefined;
let voiceTimer: number | undefined;
let discordQueueTimer: number | undefined;
let playheadTimer: number | undefined;
let volTimer: number | undefined;
let keysBound = false;
let audioBound = false;
let currentMeta: PlayerTrack | undefined;
let nextMeta: PlayerTrack | undefined;
let skipLocalStart = false;
let advancing = false;
let queueGate: Promise<unknown> = Promise.resolve();
let lastQueueMutAt = 0;
let session: SessionView = initialSession();
let lastClock: ClockSample | null = null;
let queueSse: QueueSseClient | null = null;

const commands = createCommandClient((body) => api.post<PlayerQueue>("/api/v1/me/queue/control", body));

function enqueueQueueOp<T>(fn: () => Promise<T>): Promise<T> {
  const next = queueGate.then(fn, fn);
  queueGate = next.then(
    () => undefined,
    () => undefined
  );
  return next;
}

function alreadyQueuedAtEnd(ids: string[]) {
  if (!ids.length) return false;
  const items = idsOf(usePlayer.getState().queue);
  if (ids.length > items.length) return false;
  const tail = items.slice(items.length - ids.length);
  return tail.every((id, i) => id === ids[i]);
}

function shouldSkipDupAppend(ids: string[]) {
  return Date.now() - lastQueueMutAt < 1000 && alreadyQueuedAtEnd(ids);
}

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
    muted: false,
    output_pref: "browser",
    autoplay: false,
    renderer_kind: "none",
    renderer_id: null,
    playback_instance_id: null,
    state_revision: 0,
    playhead_sequence: 0,
    ...partial
  };
}

function idsOf(q: PlayerQueue | null | undefined) {
  return q?.items?.map((i) => i.track_id) || [];
}

function idsOfSavable(q: PlayerQueue | null | undefined) {
  const keep = new Set(["ready", "restoring", "retrying", undefined, ""]);
  return (q?.items || [])
    .filter((i) => keep.has(i.media_state || ""))
    .map((i) => i.track_id);
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v);
}

function numOrUndef(v: unknown): number | undefined {
  const n = typeof v === "number" ? v : typeof v === "string" && v.trim() ? Number(v) : NaN;
  return Number.isFinite(n) ? n : undefined;
}

function asRequestedBy(raw: unknown): RequestedBy | undefined {
  if (!isRecord(raw)) return undefined;
  const user_id = typeof raw.user_id === "string" ? raw.user_id : undefined;
  const discord_user_id = typeof raw.discord_user_id === "string" ? raw.discord_user_id : undefined;
  const display_name = typeof raw.display_name === "string" ? raw.display_name : undefined;
  if (!user_id && !discord_user_id && !display_name) return undefined;
  return { user_id, discord_user_id, display_name };
}

function asQueueItem(raw: unknown): PlayerQueueItem | null {
  if (!isRecord(raw)) return null;
  const track_id = typeof raw.track_id === "string" ? raw.track_id : "";
  const id = typeof raw.id === "string" ? raw.id : track_id;
  if (!id && !track_id) return null;
  const position = numOrUndef(raw.position) ?? 0;
  const item: PlayerQueueItem = { id: id || track_id, position, track_id: track_id || id };
  if (typeof raw.origin === "string") item.origin = raw.origin;
  if (
    raw.media_state === "ready" ||
    raw.media_state === "restoring" ||
    raw.media_state === "retrying" ||
    raw.media_state === "failed" ||
    raw.media_state === "cancelled" ||
    raw.media_state === "missing_external"
  ) {
    item.media_state = raw.media_state;
  }
  if (typeof raw.intent_id === "string") item.intent_id = raw.intent_id;
  if (typeof raw.youtube_id === "string") item.youtube_id = raw.youtube_id;
  if (typeof raw.external_id === "string") item.external_id = raw.external_id;
  if (typeof raw.title === "string" && raw.title.trim()) item.title = raw.title;
  if (typeof raw.artist === "string" && raw.artist.trim()) item.artist = raw.artist;
  const duration = numOrUndef(raw.duration_ms);
  if (duration != null) item.duration_ms = duration;
  const requested = asRequestedBy(raw.requested_by);
  if (requested) item.requested_by = requested;
  return item;
}

function asQueueItems(raw: unknown): PlayerQueueItem[] {
  if (!Array.isArray(raw)) return [];
  const out: PlayerQueueItem[] = [];
  for (const row of raw) {
    const item = asQueueItem(row);
    if (item) out.push(item);
  }
  return out;
}

function cloneQueueItems(items: PlayerQueueItem[]): PlayerQueueItem[] {
  return items.map((i) => ({
    ...i,
    requested_by: i.requested_by ? { ...i.requested_by } : undefined
  }));
}

function firstNonEmptyItems(...candidates: unknown[]): PlayerQueueItem[] {
  for (const c of candidates) {
    const items = asQueueItems(c);
    if (items.length) return items;
  }
  return [];
}

/** Read undo snapshot from control/GET. Do not treat queue `items` as removed rows. */
function parseUndoPayload(result: unknown, fallbackItems: PlayerQueueItem[]): PendingUndo | null {
  if (!isRecord(result)) return null;
  const nested = isRecord(result.undo) ? result.undo : null;
  const items = firstNonEmptyItems(nested?.items, result.undo_items, result.removed_items, fallbackItems);
  if (!items.length) return null;
  const gen =
    numOrUndef(nested?.undo_generation) ??
    numOrUndef(nested?.generation) ??
    numOrUndef(result.undo_generation) ??
    numOrUndef(result.state_revision);
  if (gen == null) return null;
  return { undo_generation: gen, items: cloneQueueItems(items) };
}

function snapshotRemovedItems(action: string, extra: Record<string, unknown> | undefined, queue: PlayerQueue | null): PlayerQueueItem[] {
  const items = queue?.items;
  if (!items?.length) return [];
  if (action === "remove") {
    const pos = numOrUndef(extra?.position);
    if (pos == null) return [];
    const hit = items.find((i) => i.position === pos) ?? items[pos];
    return hit ? cloneQueueItems([hit]) : [];
  }
  if (action === "clear") {
    if (extra?.all === true) return cloneQueueItems(items);
    const idx = queue?.current_index ?? 0;
    return cloneQueueItems(items.slice(idx + 1));
  }
  return [];
}

function errorStatus(e: unknown): number | undefined {
  if (typeof e === "object" && e !== null && "status" in e) {
    const s = (e as { status: unknown }).status;
    return typeof s === "number" ? s : undefined;
  }
  return undefined;
}

function syncPendingUndo(revision: number | undefined) {
  if (typeof revision !== "number" || !Number.isFinite(revision)) return;
  const pending = usePlayer.getState().pendingUndo;
  if (!pending) return;
  if (pending.undo_generation !== revision) usePlayer.setState({ pendingUndo: null });
}

function offerUndoToast(action: string) {
  toast(action === "clear" ? "Up next cleared" : "Removed from queue", {
    action: {
      label: "Undo",
      onClick: () => {
        void usePlayer.getState().undo();
      }
    }
  });
}

function itemMediaState(q: { items?: PlayerQueueItem[] } | null | undefined, trackId: string | null | undefined): string | undefined {
  if (!q?.items || !trackId) return undefined;
  return q.items.find((i) => i.track_id === trackId)?.media_state;
}

function mediaBecameReady(
  prevItems: PlayerQueueItem[] | undefined,
  q: { items?: PlayerQueueItem[] } | null | undefined,
  trackId: string | null | undefined
): boolean {
  if (!trackId) return false;
  return !isMediaReady(itemMediaState({ items: prevItems }, trackId)) && isMediaReady(itemMediaState(q, trackId));
}

function sameTrackId(a: string | null | undefined, b: string | null | undefined): boolean {
  return String(a || "") === String(b || "");
}

function queueNeedsLocalBind(
  prevId: string | null | undefined,
  prevItems: PlayerQueueItem[] | undefined,
  q: PlayerQueue
): boolean {
  const id = q.current_track_id;
  if (!id) return false;
  if (!sameTrackId(id, prevId)) return true;
  return mediaBecameReady(prevItems, q, id);
}

/** Add / reorder / similar: items changed, current track and transport did not. */
function isQueueStructureUpdate(prev: PlayerQueue | null | undefined, next: PlayerQueue): boolean {
  if (!prev) return false;
  if (!sameTrackId(prev.current_track_id, next.current_track_id)) return false;
  if ((prev.current_index ?? 0) !== (next.current_index ?? 0)) return false;
  if ((prev.status || "") !== (next.status || "")) return false;
  if ((prev.playback_instance_id || "") !== (next.playback_instance_id || "")) return false;
  if (prev.output_pref && next.output_pref && prev.output_pref !== next.output_pref) return false;
  if (prev.renderer_kind && next.renderer_kind && prev.renderer_kind !== next.renderer_kind) return false;
  if (prev.renderer_id && next.renderer_id && prev.renderer_id !== next.renderer_id) return false;
  return true;
}

function preloadUpcoming(q: { items?: PlayerQueueItem[]; current_index?: number } | null | undefined) {
  const next = q?.items?.[(q?.current_index ?? 0) + 1];
  if (next?.track_id) preloadTrack(next.track_id, next.media_state);
}

function playingAfterStart(played: boolean, wantPlay: boolean, trackId: string | null | undefined): boolean {
  if (!wantPlay) return false;
  if (played) return true;
  return !isMediaReady(itemMediaState(session.queue, trackId));
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

function publishMediaPosition() {
  const s = usePlayer.getState();
  updateMediaPosition({
    duration: s.duration,
    playbackRate: s.playbackRate,
    position: s.position,
    playing: s.playing
  });
}

async function joinDiscord() {
  const expected = session.lastBindingRevision || session.queue.binding_revision;
  return api.post<{ ok?: boolean; guild_id?: string; channel_id?: string; binding_revision?: number; state_revision?: number; session_id?: string }>(
    "/api/v1/me/discord/join",
    {
      device_id: getDeviceId(),
      ...(expected ? { expected_binding_revision: expected } : {})
    }
  );
}

function guildIdOf(): string {
  return usePlayer.getState().voice?.guild_id || "";
}

function tabId() {
  return getTabRendererId();
}

function usingDiscord() {
  const s = usePlayer.getState();
  if (loadDevicePrefs().outputManual === "browser") return false;
  return s.queue?.output_pref === "discord" || s.queue?.renderer_kind === "discord" || s.queue?.kind === "discord_guild";
}

function discordBlocked() {
  const s = usePlayer.getState();
  return usingDiscord() && !discordReady(s.voice);
}

function interpolatedNow(): number {
  const ph = session.playhead;
  return interpolatePosition({
    playing: ph.playing,
    checkpointPositionMs: ph.checkpointPositionMs,
    checkpointAtMs: ph.checkpointAtMs,
    playbackRate: ph.playbackRate,
    durationMs: ph.durationMs || usePlayer.getState().duration,
    nowMs: Date.now(),
    offsetMs: session.clockOffsetMs
  });
}

function patchFromSession(view: SessionView, extra: Record<string, unknown> = {}) {
  const q = view.queue as PlayerQueue;
  const output = q.output_pref === "discord" || q.output_pref === "browser" ? q.output_pref : usePlayer.getState().output;
  return {
    queue: q,
    playing: q.status === "playing",
    volume: q.volume ?? usePlayer.getState().volume,
    muted: q.muted ?? usePlayer.getState().muted,
    shuffle: !!q.shuffle,
    repeat: q.repeat || "off",
    stopAfterCurrent: !!q.stop_after_current,
    position: Number.isFinite(view.playhead.positionMs) ? view.playhead.positionMs : q.position_ms || 0,
    duration: q.duration_ms || view.playhead.durationMs || usePlayer.getState().duration,
    playbackRate: q.playback_rate && q.playback_rate > 0 ? q.playback_rate : usePlayer.getState().playbackRate,
    autoplay: q.autoplay ?? usePlayer.getState().autoplay,
    output,
    ...extra
  };
}

function ingestQueue(snap: QueueSnapshot, opts?: { clock?: ClockSample; kind?: "snapshot" | "playhead" | "bind" }): SessionView {
  const prev = session;
  session = applySnapshot(prev, snap, {
    guildId: guildIdOf(),
    clock: opts?.clock ?? lastClock ?? undefined,
    tabRendererId: tabId(),
    nowMs: Date.now(),
    kind: opts?.kind
  });
  if (opts?.clock) lastClock = opts.clock;
  if (session.stopAudio || shouldStopHtmlAudio(session.queue, tabId())) pauseAll();
  const listeners = pickListeners(snap);
  if (listeners) usePlayer.setState({ listeners });
  syncPendingUndo((session.queue as PlayerQueue).state_revision);
  return session;
}

function applyRemoteQueue(snap: QueueSnapshot, opts?: { clock?: ClockSample; kind?: "snapshot" | "playhead" }) {
  const prev = usePlayer.getState();
  const prevId = prev.queue?.current_track_id;
  const prevItems = prev.queue?.items;
  const view = ingestQueue(snap, opts);
  const q = session.queue as PlayerQueue;
  const own = !usingDiscord() && tabOwnsBrowserLease(session.queue, tabId());
  const structureOnly = opts?.kind !== "playhead" && isQueueStructureUpdate(prev.queue, q);
  const mediaReady = mediaBecameReady(prevItems, q, q.current_track_id);
  const keepTransport = structureOnly && !view.stopAudio && !mediaReady;

  if (opts?.kind === "playhead" && own) {
    usePlayer.setState({
      duration: view.playhead.durationMs || prev.duration,
      playbackRate: view.playhead.playbackRate || prev.playbackRate
    });
  } else {
    const jump = Math.abs(view.playhead.positionMs - prev.position) > 1500;
    const position =
      keepTransport || !(view.stopAudio || usingDiscord() || !own || jump) ? prev.position : view.playhead.positionMs;
    usePlayer.setState({
      ...patchFromSession(view, keepTransport ? { playing: prev.playing } : {}),
      position
    });
  }

  if (!keepTransport && (view.stopAudio || shouldStopHtmlAudio(session.queue, tabId()))) pauseAll();

  if (opts?.kind === "playhead") {
    if (!own) {
      usePlayer.setState({ playing: session.queue.status === "playing", position: interpolatedNow() });
    }
    ensureSessionPoll();
    return;
  }

  const becamePaused =
    (q.status === "paused" || q.status === "stopped") && (prev.queue?.status === "playing" || prev.playing);
  if (!keepTransport && (view.stopAudio || becamePaused)) {
    pauseAll();
    usePlayer.setState({ playing: false });
  }

  if (!keepTransport && own && queueNeedsLocalBind(prevId, prevItems, q) && q.current_track_id) {
    const id = q.current_track_id;
    void usePlayer
      .getState()
      .hydrateTrack(id)
      .then(async (t) => {
        const ownNow = !usingDiscord() && tabOwnsBrowserLease(session.queue, tabId());
        if (!ownNow) {
          pauseAll();
          return;
        }
        const wantPlay = session.queue.status === "playing";
        const played = await startLocal(id, interpolatedNow(), wantPlay, t);
        if (wantPlay) usePlayer.setState({ playing: playingAfterStart(played, true, id) });
      });
  }
  if (own) preloadUpcoming(q);
  ensureSessionPoll();
}

function ensureQueueSse(): QueueSseClient {
  if (queueSse) return queueSse;
  queueSse = createQueueSseClient({
    fetchSnapshot: async () => {
      const { queue } = await fetchQueue();
      return { queue, listeners: pickListeners(queue) };
    },
    onSnapshot: ({ queue }) => {
      applyRemoteQueue(queue, { clock: lastClock ?? undefined, kind: "snapshot" });
    },
    onState: (snap) => {
      const serverTime = parseTimeMs(snap.server_time);
      const clock = serverTime != null ? sampleClock(Date.now(), Date.now(), serverTime) : undefined;
      applyRemoteQueue(snap, { clock, kind: "snapshot" });
    },
    onPlayhead: (event) => {
      applyRemoteQueue(playheadEventToSnap(event, session.queue), { kind: "playhead" });
    },
    onPresence: (event) => {
      usePlayer.setState({ listeners: mergePresence(usePlayer.getState().listeners, event) });
    },
    onInvalidate: (event) => {
      for (const key of event.keys || []) {
        void queryClient.invalidateQueries({ queryKey: [key] });
      }
    },
    onJobProgress: () => undefined,
    onAuthLost: () => {
      queueSse?.stop();
    }
  });
  return queueSse;
}

async function fetchQueue(): Promise<{ queue: PlayerQueue; clock: ClockSample }> {
  const localSend = Date.now();
  const q = ((await api.get<PlayerQueue>("/api/v1/me/queue")) || emptyQueue()) as PlayerQueue;
  const localReceive = Date.now();
  const serverTime = parseTimeMs((q as QueueSnapshot).server_time);
  const clock = sampleClock(localSend, localReceive, serverTime);
  lastClock = clock;
  return { queue: q, clock };
}

function ensureSessionPoll() {
  if (usingDiscord() && !discordQueueTimer) {
    discordQueueTimer = window.setInterval(() => {
      usePlayer.getState().syncDiscordQueue();
    }, 1000);
  }
  if (!usingDiscord() && discordQueueTimer) {
    window.clearInterval(discordQueueTimer);
    discordQueueTimer = undefined;
  }
  if ((usingDiscord() || !tabOwnsBrowserLease(session.queue, tabId())) && !playheadTimer) {
    playheadTimer = window.setInterval(() => {
      if (seeking) return;
      usePlayer.setState({ position: interpolatedNow() });
      publishMediaPosition();
    }, 250);
  }
  if (!usingDiscord() && tabOwnsBrowserLease(session.queue, tabId()) && playheadTimer) {
    window.clearInterval(playheadTimer);
    playheadTimer = undefined;
  }
}

async function acquireBrowserLease(): Promise<boolean> {
  const id = tabId();
  if (tabOwnsBrowserLease(session.queue, id) && session.queue.output_pref !== "discord") return true;
  askRendererTabsToStop();
  try {
    const res = await api.post<QueueSnapshot>("/api/v1/me/queue/renderer/acquire", {
      renderer_id: id,
      expected_generation: 0,
      device_id: getDeviceId()
    });
    if (res) ingestQueue(res);
    usePlayer.setState(patchFromSession(session));
    return tabOwnsBrowserLease(session.queue, id);
  } catch {
    return tabOwnsBrowserLease(session.queue, id);
  }
}

async function startLocal(id: string, positionMsValue: number, shouldPlay: boolean, meta?: PlayerTrack) {
  const mediaState = itemMediaState(session.queue, id) ?? itemMediaState(usePlayer.getState().queue, id);
  if (!isMediaReady(mediaState)) {
    stopElement(getAudio());
    if (shouldPlay && !usingDiscord() && !shouldStopHtmlAudio(session.queue, tabId())) {
      await acquireBrowserLease();
    }
    return false;
  }
  const a = getAudio();
  await bindTrack(a, id, mediaState);
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
    if (a.dataset.trackId === id && a.src && !a.paused) return true;
    try {
      a.pause();
    } catch {
      /* ignore */
    }
    return false;
  }
  if (usingDiscord() || shouldStopHtmlAudio(session.queue, tabId())) {
    pauseAll();
    return false;
  }
  const owns = await acquireBrowserLease();
  if (!owns || !tabOwnsBrowserLease(session.queue, tabId())) {
    pauseAll();
    return false;
  }
  try {
    await playActive();
    return true;
  } catch {
    return false;
  }
}

function isPlaybackLive() {
  const s = usePlayer.getState();
  if (s.playing) return true;
  const status = s.queue?.status;
  return status === "paused" || status === "playing" || status === "interrupted";
}

function hintPayload(ids: string[], hints?: QueueTrackHint[]) {
  if (!hints?.length) return undefined;
  const want = new Set(ids);
  return hints.filter((h) => h.id && want.has(h.id));
}

function canPlayOnDiscord() {
  return loadDevicePrefs().outputManual !== "browser" && discordReady(usePlayer.getState().voice);
}

function dropToBrowser() {
  session = applyJoinFailure(session);
  usePlayer.setState({ output: "browser", queue: { ...(usePlayer.getState().queue || emptyQueue()), output_pref: "browser" } });
}

async function playOnBrowser(ids: string[], idx: number, hints?: QueueTrackHint[]) {
  const q = await api.put<PlayerQueue>("/api/v1/me/queue", {
    track_ids: ids,
    tracks: hintPayload(ids, hints),
    start: idx,
    device_id: getDeviceId(),
    command_id: newCommandId()
  });
  lastQueueMutAt = Date.now();
  ingestQueue(q);
  usePlayer.setState(patchFromSession(session, { playing: true, output: "browser" }));
  const id = q.current_track_id || ids[idx];
  const hint = hints?.[idx] || hints?.find((h) => h.id === ids[idx]);
  if (hint?.title) {
    currentMeta = { id, title: hint.title, artist: hint.artist, duration_ms: hint.duration_ms };
    usePlayer.setState({ current: currentMeta, duration: hint.duration_ms || 0 });
  } else {
    currentMeta = undefined;
  }
  listen = null;
  const t = await usePlayer.getState().hydrateTrack(id);
  const played = await startLocal(id, 0, true, t);
  usePlayer.setState({ playing: playingAfterStart(played, true, id), output: "browser" });
  if (played) beginListen(id);
  preloadUpcoming(session.queue);
}

async function appendToQueue(ids: string[], next?: boolean, hints?: QueueTrackHint[]) {
  if (!ids.length) return false;
  if (shouldSkipDupAppend(ids)) return false;
  if (ids.some((id) => !isLibraryTrackId(id))) {
    toast.message("Getting it from YouTube…");
  }
  await usePlayer.getState().pollVoice();
  if (discordReady(usePlayer.getState().voice) && loadDevicePrefs().outputManual !== "browser") {
    try {
      await joinDiscord();
    } catch {
      usePlayer.setState({ output: "browser" });
    }
  } else if (loadDevicePrefs().outputManual !== "browser") {
    usePlayer.setState({ output: "browser" });
  }
  const prev = usePlayer.getState();
  lastQueueMutAt = Date.now();
  const q = await api.post<PlayerQueue>("/api/v1/me/queue/add", {
    track_ids: ids,
    tracks: hintPayload(ids, hints),
    next,
    device_id: getDeviceId(),
    command_id: newCommandId()
  });
  lastQueueMutAt = Date.now();
  const applyAdded = (snap: PlayerQueue) => {
    ingestQueue(snap);
    const same = sameTrackId(session.queue.current_track_id, prev.queue?.current_track_id);
    usePlayer.setState(
      patchFromSession(session, {
        playing: same ? prev.playing || session.queue.status === "playing" : session.queue.status === "playing",
        ...(same ? { position: prev.position } : {})
      })
    );
  };
  if (q && Array.isArray(q.items)) {
    applyAdded(q);
    return true;
  }
  try {
    const { queue: fresh, clock } = await fetchQueue();
    lastClock = clock;
    applyAdded(fresh);
  } catch {
    /* keep local queue */
  }
  return true;
}

async function maybeReplenishRadio() {
  const s = usePlayer.getState();
  if (!s.autoplay || s.stopAfterCurrent || radioBusy) return;
  const items = s.queue?.items || [];
  const idx = s.queue?.current_index ?? 0;
  if (!items.length || items.length - idx > 2) return;
  const seed = s.current?.id;
  if (!seed || !isLibraryTrackId(seed)) return;
  radioBusy = true;
  try {
    const have = new Set(items.map((it) => it.track_id));
    have.add(seed);
    const exclude = [...have].filter((id) => isLibraryTrackId(id)).slice(-80);
    const r = await api.get<{ track_ids?: string[]; youtube_ids?: string[] }>(
      `/api/v1/radio?kind=track&seed_id=${encodeURIComponent(seed)}&limit=8&fill=youtube&exclude=${exclude.map(encodeURIComponent).join(",")}&recent=40`
    );
    const extra = (r.track_ids || []).filter((id) => id && !have.has(id));
    if (extra.length) await usePlayer.getState().add(extra, false);
    const yt = (r.youtube_ids || []).filter(Boolean).slice(0, 6);
    for (let i = 0; i < yt.length; i += 4) {
      const live = usePlayer.getState();
      if (!live.autoplay || live.stopAfterCurrent) break;
      await live.add(yt.slice(i, i + 4), false);
    }
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
  keyboardShortcuts: boolean;
  stopAfterCurrent: boolean;
  sleepUntil: number | null;
  output: OutputTarget;
  voice: VoiceState | null;
  sinkId: string;
  listeners: PresenceParticipant[];
  pendingUndo: PendingUndo | null;
  load: () => Promise<void>;
  playTracks: (ids: string[], start?: number, hints?: QueueTrackHint[]) => Promise<void>;
  playNow: (index: number) => Promise<void>;
  add: (ids: string[], next?: boolean, hints?: QueueTrackHint[]) => Promise<void>;
  control: (action: string, extra?: Record<string, unknown>) => Promise<void>;
  seek: (ms: number) => void;
  setVolume: (v: number) => void;
  toggleMute: () => void;
  hydrateTrack: (id: string) => Promise<PlayerTrack | undefined>;
  setOutput: (o: OutputTarget) => Promise<void>;
  pollVoice: () => Promise<void>;
  syncDiscordQueue: () => Promise<void>;
  setPlaybackRate: (r: number) => void;
  setAutoplay: (on: boolean) => void;
  setVisualizer: (on: boolean) => void;
  setTinyMode: (on: boolean) => void;
  setKeyboardShortcuts: (on: boolean) => void;
  setStopAfterCurrent: (on: boolean) => Promise<void>;
  setSleep: (minutes: number | null) => void;
  setSink: (id: string) => Promise<void>;
  saveQueueAsPlaylist: (name: string) => Promise<void>;
  undo: () => Promise<void>;
};

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
      keyboardShortcuts: false,
      stopAfterCurrent: false,
      sleepUntil: null,
      output: prefs.outputManual === "browser" ? "browser" : "discord",
      voice: null,
      sinkId: prefs.sinkId,
      listeners: [],
      pendingUndo: null,
      load: async () => {
        try {
          await get().pollVoice();
          const { queue: q, clock } = await fetchQueue();
          ingestQueue(q || emptyQueue(), { clock });
          const discord = usingDiscord() || shouldStopHtmlAudio(session.queue, tabId());
          set(patchFromSession(session, { playing: discord ? session.queue.status === "playing" : false }));
          applyVolume(get().volume, get().muted);
          if (q.current_track_id) {
            const t = await get().hydrateTrack(q.current_track_id);
            if (discord) {
              pauseAll();
              set({ playing: session.queue.status === "playing", position: interpolatedNow(), duration: t?.duration_ms || session.playhead.durationMs || 0 });
            } else {
              const wantPlay =
                session.queue.status === "playing" &&
                (tabOwnsBrowserLease(session.queue, tabId()) || !session.queue.renderer_id || session.queue.renderer_kind === "none");
              const played = wantPlay ? await startLocal(q.current_track_id, interpolatedNow(), true, t) : await startLocal(q.current_track_id, interpolatedNow(), false, t);
              set({ playing: playingAfterStart(played, wantPlay, q.current_track_id), position: interpolatedNow(), duration: t?.duration_ms || durationMs() });
              if (played) beginListen(q.current_track_id);
            }
          }
          const nextItem = q.items?.[(q.current_index ?? 0) + 1];
          if (nextItem?.track_id && !discord) {
            preloadTrack(nextItem.track_id, nextItem.media_state);
            get()
              .hydrateTrack(nextItem.track_id)
              .then((t) => {
                nextMeta = t;
              })
              .catch(() => undefined);
          }
          ensureSessionPoll();
          ensureQueueSse().start({ resync: false });
        } catch {
          /* unauthenticated or not in voice */
        }
      },
      hydrateTrack: async (id) => {
        try {
          const t = await api.get<PlayerTrack>(`/api/v1/tracks/${id}`);
          if (get().queue?.current_track_id === id || get().current?.id === id || !get().current) {
            currentMeta = t;
            set({ current: t, duration: t.duration_ms || durationMs() || 0 });
            bindMediaSession(t);
            publishMediaPosition();
            applyReplayGain(replayGainMultiplier(get().queue?.replaygain_mode, t));
          }
          return t;
        } catch {
          const fallback: PlayerTrack = { id, title: "Track" };
          if (!get().current || get().current?.id === id) set({ current: fallback });
          return fallback;
        }
      },
      playTracks: async (ids, start = 0, hints) => {
        if (!ids.length) return;
        const idx = Math.max(0, Math.min(start, ids.length - 1));
        await enqueueQueueOp(async () => {
          if (isPlaybackLive()) {
            const toAdd = ids.slice(idx);
            const added = await appendToQueue(toAdd, false, hints?.slice(idx));
            if (added) toast.success(toAdd.length === 1 ? "Added to queue" : `Added ${toAdd.length} tracks to queue`);
            return;
          }
          if (ids.some((id) => !isLibraryTrackId(id))) {
            toast.message("Getting it from YouTube…");
          }
          await get().pollVoice();
          if (canPlayOnDiscord()) {
            pauseAll();
            try {
              const joined = await joinDiscord();
              const next = applyBindResult(session, joined || {}, joined?.guild_id || guildIdOf());
              if (next.ignored === "stale_bind") {
                dropToBrowser();
              } else {
                session = next;
                const q = await api.put<PlayerQueue>("/api/v1/me/queue", {
                  track_ids: ids,
                  tracks: hintPayload(ids, hints),
                  start: idx,
                  device_id: getDeviceId(),
                  command_id: newCommandId()
                });
                ingestQueue(q || emptyQueue());
                set(patchFromSession(session, { playing: true, position: 0, output: "discord" }));
                lastQueueMutAt = Date.now();
                const currentId = usePlayer.getState().queue?.current_track_id || ids[idx];
                await get().hydrateTrack(currentId);
                ensureSessionPoll();
                return;
              }
            } catch {
              dropToBrowser();
            }
          } else if (get().output === "discord") {
            dropToBrowser();
          }
          await playOnBrowser(ids, idx, hints);
        });
      },
      playNow: async (index) => {
        const q = get().queue;
        const ids = idsOf(q);
        if (!q || index < 0 || index >= ids.length) return;
        await get().pollVoice();
        if (canPlayOnDiscord()) {
          pauseAll();
          try {
            await joinDiscord();
            await get().control("index", { index });
            return;
          } catch {
            dropToBrowser();
          }
        } else if (get().output === "discord") {
          dropToBrowser();
        }
        listen = null;
        let jumped = false;
        try {
          await get().control("index", { index });
          jumped = get().queue?.current_track_id === ids[index];
        } catch {
          /* P1 may not have index yet - still jump locally, never PUT-replace */
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
        set({ playing: playingAfterStart(played, true, ids[index]) });
        if (played) beginListen(ids[index]);
        preloadUpcoming(get().queue);
      },
      add: async (ids, next, hints) => {
        await enqueueQueueOp(() => appendToQueue(ids, next, hints));
      },
      control: async (action, extra) => {
        if (action === "pause") {
          pauseAll();
          extra = { ...extra, position_ms: Math.round(get().position) };
        }
        const localRemoved =
          action === "remove" || action === "clear" ? snapshotRemovedItems(action, extra, get().queue) : [];
        let q: PlayerQueue | null;
        try {
          q = (await commands.control(action, extra || {}, getDeviceId())) as PlayerQueue | null;
        } catch (e) {
          if (action === "undo" && errorStatus(e) === 409) {
            toast.error("Undo expired");
            set({ pendingUndo: null });
            return;
          }
          if (action === "index") throw e;
          if (action === "stop_after_current" || action === "reorder" || action === "seek") return;
          toast.error(e instanceof Error ? e.message : "Playback control failed");
          return;
        }
        if (!q) return;
        const prevId = get().current?.id;
        const prevItems = get().queue?.items;
        const metaOnly =
          action === "seek" ||
          action === "reorder" ||
          action === "volume" ||
          action === "mute" ||
          action === "unmute" ||
          action === "stop_after_current" ||
          action === "remove" ||
          action === "clear" ||
          action === "undo";
        ingestQueue(q);
        if (action === "remove" || action === "clear") {
          const pending = parseUndoPayload(q, localRemoved);
          set({ pendingUndo: pending });
          if (pending) offerUndoToast(action);
        } else if (action === "undo") {
          set({ pendingUndo: null });
        }
        set(patchFromSession(session, metaOnly ? { playing: get().playing } : {}));
        const discord = usingDiscord();
        if (!metaOnly && (action === "pause" || q.status === "paused" || q.status === "stopped")) {
          pauseAll();
          set({ playing: false });
        }
        if (action === "resume" && !discord) {
          try {
            if (!(await acquireBrowserLease())) {
              pauseAll();
              set({ playing: false });
              return;
            }
            const resumeId = q.current_track_id || get().current?.id;
            if (resumeId && !isMediaReady(itemMediaState(session.queue, resumeId))) {
              stopElement(getAudio());
              set({ playing: true });
              return;
            }
            await playActive();
            set({ playing: true });
            if (get().current?.id) beginListen(get().current!.id);
          } catch {
            set({ playing: false });
          }
        }
        if (action === "resume" && discord) {
          pauseAll();
          set({ playing: q.status === "playing" });
        }
        if (action === "stop") {
          pauseAll();
          set({ playing: false, position: 0 });
        }
        if (queueNeedsLocalBind(prevId, prevItems, q) && q.current_track_id) {
          const t = await get().hydrateTrack(q.current_track_id);
          currentMeta = t;
          if (skipLocalStart) {
            applyReplayGain(replayGainMultiplier(get().queue?.replaygain_mode, t));
          } else if (!discord && q.status === "playing") {
            const played = await startLocal(q.current_track_id, q.position_ms || interpolatedNow(), true, t);
            set({ playing: playingAfterStart(played, true, q.current_track_id) });
            if (played) {
              listen = null;
              beginListen(q.current_track_id);
            }
          } else if (!discord) {
            await startLocal(q.current_track_id, q.position_ms || interpolatedNow(), false, t);
          } else {
            pauseAll();
          }
          preloadUpcoming(q);
          const nid = q.items?.[(q.current_index ?? 0) + 1]?.track_id;
          if (nid) {
            get()
              .hydrateTrack(nid)
              .then((m) => {
                nextMeta = m;
              })
              .catch(() => undefined);
          }
        }
        ensureSessionPoll();
      },
      seek: (ms) => {
        seeking = true;
        if (!usingDiscord()) seekActive(ms);
        set({ position: ms });
        seeking = false;
        const t = get().current;
        if (t) markProgress(t.id, ms, get().duration);
        get().control("seek", { position_ms: Math.round(ms) }).catch(() => undefined);
        publishMediaPosition();
      },
      setVolume: (v) => {
        const next = Math.min(1, Math.max(0, v));
        const muted = next <= 0;
        applyVolume(next, muted);
        set({ volume: next, muted });
        if (volTimer) window.clearTimeout(volTimer);
        volTimer = window.setTimeout(() => {
          get().control("volume", { volume: next }).catch(() => undefined);
        }, 150);
      },
      toggleMute: () => {
        const muted = !get().muted;
        applyVolume(get().volume, muted);
        set({ muted });
        get().control(muted ? "mute" : "unmute").catch(() => undefined);
      },
      setOutput: async (o) => {
        if (o === "discord") {
          const wasPlaying = get().playing || get().queue?.status === "playing";
          const resumeId = get().current?.id;
          const resumePos = get().position;
          const resumeMeta = get().current;
          pauseAll();
          setManualOutput("discord");
          await get().pollVoice();
          if (!discordReady(get().voice)) {
            session = applyJoinFailure(session);
            set({ output: "browser", playing: false, queue: { ...get().queue, output_pref: "browser" } as PlayerQueue });
            toast.error("Join a Discord voice channel to play");
            if (wasPlaying && resumeId) {
              const played = await startLocal(resumeId, resumePos, true, resumeMeta);
              set({ playing: played, output: "browser" });
            }
            ensureSessionPoll();
            return;
          }
          try {
            const joined = await joinDiscord();
            const next = applyBindResult(session, joined || {}, joined?.guild_id || guildIdOf());
            if (next.ignored === "stale_bind") {
              session = applyJoinFailure(session);
              set({ output: "browser", queue: { ...get().queue, output_pref: "browser" } as PlayerQueue });
              toast.error("Voice bind is out of date");
              if (wasPlaying && resumeId) {
                const played = await startLocal(resumeId, resumePos, true, resumeMeta);
                set({ playing: played });
              }
              ensureSessionPoll();
              return;
            }
            session = next;
            pauseAll();
            try {
              const switched = (await commands.control(
                "output_pref",
                { output_pref: "discord" },
                getDeviceId()
              )) as PlayerQueue | null;
              if (switched) {
                ingestQueue(switched);
              }
            } catch {
              /* bind succeeded; worker claims when output_pref is discord */
            }
            set(patchFromSession(session, { output: "discord", playing: session.queue.status === "playing" || wasPlaying }));
          } catch (e) {
            session = applyJoinFailure(session);
            set({ output: "browser", queue: { ...(get().queue || emptyQueue()), output_pref: "browser" } });
            toast.error(e instanceof Error ? e.message : "Discord join failed");
            if (wasPlaying && resumeId) {
              const played = await startLocal(resumeId, resumePos, true, resumeMeta);
              set({ playing: played, output: "browser" });
            }
            ensureSessionPoll();
            return;
          }
          ensureSessionPoll();
          return;
        }
        setManualOutput("browser");
        try {
          const switched = (await commands.control(
            "output_pref",
            { output_pref: "browser", renderer_id: tabId() },
            getDeviceId()
          )) as PlayerQueue | null;
          if (switched) {
            const bindRev = session.lastBindingRevision;
            const byGuild = session.lastBindingByGuild;
            session = applySwitchToBrowser(session, switched);
            session = { ...session, lastBindingRevision: bindRev, lastBindingByGuild: byGuild };
            set(patchFromSession(session, { output: "browser" }));
          }
        } catch (e) {
          toast.error(e instanceof Error ? e.message : "Could not switch to browser");
          ensureSessionPoll();
          return;
        }
        const owned = await acquireBrowserLease();
        ensureSessionPoll();
        const id = get().current?.id;
        const fromCheckpoint = interpolatedNow();
        if (owned && id && (get().playing || get().queue?.status === "playing")) {
          const played = await startLocal(id, fromCheckpoint, true, get().current);
          set({ playing: played, position: fromCheckpoint, output: "browser" });
        } else {
          set({ output: "browser", position: fromCheckpoint });
          if (id) await startLocal(id, fromCheckpoint, false, get().current);
        }
      },
      pollVoice: async () => {
        const voice = await fetchVoice();
        const manual = loadDevicePrefs().outputManual;
        const cur = get().output;
        const output =
          manual === "browser" || voice?.discord_enabled === false
            ? "browser"
            : discordReady(voice)
              ? "discord"
              : cur || "discord";
        set({ voice, output });
        ensureSessionPoll();
      },
      syncDiscordQueue: async () => {
        try {
          const { queue: q, clock } = await fetchQueue();
          if (!q) return;
          const view = ingestQueue(q, { clock });
          const cur = get();
          set({
            ...patchFromSession(view),
            position: view.stopAudio || usingDiscord() || Math.abs(view.playhead.positionMs - cur.position) > 1500 ? view.playhead.positionMs : cur.position
          });
          if (view.stopAudio) pauseAll();
          if (q.current_track_id && (q.current_track_id !== cur.queue?.current_track_id || !cur.current)) {
            await get().hydrateTrack(q.current_track_id);
          }
          const idx = q.current_index ?? 0;
          if ((q.items || []).length - idx <= 2) maybeReplenishRadio();
        } catch {
          /* 409 not in voice, or offline */
        }
      },
      setPlaybackRate: (r) => {
        const rate = Math.min(2, Math.max(0.5, r));
        applyRate(rate);
        saveDevicePrefs({ playbackRate: rate });
        set({ playbackRate: rate });
        publishMediaPosition();
      },
      setAutoplay: (on) => {
        saveDevicePrefs({ autoplay: on, autoplaySet: true });
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
      setKeyboardShortcuts: (on) => {
        usePrefs.getState().setKeyboardShortcuts(on);
        set({ keyboardShortcuts: on });
      },
      setStopAfterCurrent: async (on) => {
        set({ stopAfterCurrent: on });
        try {
          await get().control("stop_after_current", { stop_after_current: on, enabled: on });
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
        const ids = idsOfSavable(get().queue);
        if (!ids.length) return;
        const created = await api.post<{ id: string }>("/api/v1/playlists", { name: name || "Queue" });
        if (created?.id) await api.post(`/api/v1/playlists/${created.id}/tracks`, { track_ids: ids });
        toast.success("Saved as playlist");
      },
      undo: async () => {
        const pending = get().pendingUndo;
        if (!pending?.items.length) return;
        await get().control("undo", { undo_generation: pending.undo_generation, items: pending.items });
      }
    }),
    { name: "sd-player", partialize: (s) => ({ volume: s.volume, muted: s.muted }) }
  )
);

usePrefs.persist.onFinishHydration((s) => {
  usePlayer.setState({ keyboardShortcuts: s?.keyboardShortcuts === true });
});
if (usePrefs.persist.hasHydrated()) {
  usePlayer.setState({ keyboardShortcuts: usePrefs.getState().keyboardShortcuts === true });
}

function onTimeUpdate(el: HTMLAudioElement) {
  if (usingDiscord() || el !== getAudio() || seeking) return;
  if (shouldStopHtmlAudio(session.queue, tabId())) {
    pauseAll();
    return;
  }
  const pos = positionMs();
  const dur = durationMs() || usePlayer.getState().duration;
  usePlayer.setState({ position: pos, duration: dur || usePlayer.getState().duration });
  const cur = usePlayer.getState().current;
  if (cur) markProgress(cur.id, pos, dur);
  if (Date.now() - persistPosAt > 10000 && usePlayer.getState().playing && !usingDiscord()) {
    persistPosAt = Date.now();
    usePlayer.getState().control("seek", { position_ms: Math.round(pos) }).catch(() => undefined);
  }
  if (remainingMs() < 12000) maybeReplenishRadio();
  scheduleCrossfade();
  publishMediaPosition();
}

function onEnded(el: HTMLAudioElement) {
  if (usingDiscord() || el !== getAudio()) return;
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
  const keysOn = usePrefs.getState().keyboardShortcuts === true;
  if (usePlayer.getState().keyboardShortcuts !== keysOn) {
    usePlayer.setState({ keyboardShortcuts: keysOn });
  }
  subscribeRendererChannel(() => {
    pauseAll();
  });
  if (!audioBound) {
    audioBound = true;
    const bind = (el: HTMLAudioElement) => {
      el.ontimeupdate = () => onTimeUpdate(el);
      el.onended = () => onEnded(el);
      el.onplay = () => {
        if (usingDiscord() || shouldStopHtmlAudio(session.queue, tabId())) {
          pauseAll();
          return;
        }
        if (el === getAudio()) usePlayer.setState({ playing: true });
      };
      el.onpause = () => {
        if (usingDiscord()) return;
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

  attachMediaRemote({
    play: () => usePlayer.getState().control("resume"),
    pause: () => usePlayer.getState().control("pause"),
    next: () => usePlayer.getState().control("skip"),
    previous: () => usePlayer.getState().control("previous"),
    seekTo: (ms) => usePlayer.getState().seek(ms),
    seekBy: (deltaMs) => {
      const p = usePlayer.getState();
      p.seek(Math.max(0, p.position + deltaMs));
    }
  });

  if (!keysBound) {
    keysBound = true;
    window.addEventListener("keydown", (e) => {
      if (useUi.getState().commandOpen) return;
      if (typingTarget(e.target)) return;
      if (!usePrefs.getState().keyboardShortcuts) return;
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
    voiceTimer = window.setInterval(() => usePlayer.getState().pollVoice(), 2000);
    usePlayer.getState().pollVoice();
  }
}

export function applyRemoteQueueForTests(snap: QueueSnapshot) {
  applyRemoteQueue(snap, { kind: "snapshot" });
}

export function resetPlayerSessionForTests(view?: SessionView) {
  queueSse?.stop();
  session = view ? { ...initialSession(), ...view } : initialSession();
  lastClock = null;
  commands.reset();
  lastQueueMutAt = 0;
  usePlayer.setState({ listeners: [], pendingUndo: null });
}

export function getPlayerSessionForTests() {
  return session;
}

export { getAudio, getAnalyser } from "@/components/player/audioEngine";
