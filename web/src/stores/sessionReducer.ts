import type { QueueState } from "@/types/api";
import {
  interpolatePosition,
  parseTimeMs,
  shouldAcceptRefine,
  type ClockSample
} from "@/stores/playhead";

export type QueueSnapshot = QueueState & {
  server_time?: string | number | null;
  generation?: number;
};

export type PlayheadView = {
  positionMs: number;
  checkpointPositionMs: number;
  checkpointAtMs: number;
  playbackRate: number;
  durationMs: number;
  playing: boolean;
  sequence: number;
  instanceId: string | null;
  offsetMs: number;
  stateRevision: number;
};

export type SessionView = {
  lastAppliedRevision: number;
  lastPlayheadSequence: number;
  lastInstanceId: string | null;
  lastBindingRevision: number;
  lastBindingByGuild: Record<string, number>;
  queue: QueueSnapshot;
  playhead: PlayheadView;
  clockOffsetMs: number;
  ignored: "stale_revision" | "stale_playhead" | "stale_bind" | null;
  stopAudio: boolean;
  stateApplied: boolean;
  playheadApplied: boolean;
};

export type BindResult = {
  ok?: boolean;
  guild_id?: string;
  binding_revision?: number;
  state_revision?: number;
  session_id?: string;
  playback_instance_id?: string | null;
};

export type ApplyOpts = {
  guildId?: string | null;
  clock?: ClockSample;
  tabRendererId?: string | null;
  nowMs?: number;
  kind?: "snapshot" | "playhead" | "bind";
};

function num(v: unknown, fallback = 0): number {
  const n = typeof v === "number" ? v : Number(v);
  return Number.isFinite(n) ? n : fallback;
}

function emptyQueue(): QueueSnapshot {
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
    renderer_generation: 0,
    playback_instance_id: null,
    state_revision: 0,
    playhead_sequence: 0,
    binding_revision: null,
    checkpoint_at: null,
    duration_ms: 0,
    playback_rate: 1
  };
}

function emptyPlayhead(): PlayheadView {
  return {
    positionMs: 0,
    checkpointPositionMs: 0,
    checkpointAtMs: 0,
    playbackRate: 1,
    durationMs: 0,
    playing: false,
    sequence: 0,
    instanceId: null,
    offsetMs: 0,
    stateRevision: 0
  };
}

export function initialSession(): SessionView {
  return {
    lastAppliedRevision: 0,
    lastPlayheadSequence: 0,
    lastInstanceId: null,
    lastBindingRevision: 0,
    lastBindingByGuild: {},
    queue: emptyQueue(),
    playhead: emptyPlayhead(),
    clockOffsetMs: 0,
    ignored: null,
    stopAudio: false,
    stateApplied: false,
    playheadApplied: false
  };
}

export function tabOwnsBrowserLease(
  snap: Pick<QueueSnapshot, "output_pref" | "renderer_kind" | "renderer_id"> | null | undefined,
  tabRendererId: string | null | undefined
): boolean {
  if (!snap || !tabRendererId) return false;
  if (snap.output_pref === "discord" || snap.renderer_kind === "discord") return false;
  if (snap.renderer_kind && snap.renderer_kind !== "browser" && snap.renderer_kind !== "none") return false;
  if (!snap.renderer_id) return false;
  return snap.renderer_kind === "browser" && snap.renderer_id === tabRendererId;
}

export function shouldStopHtmlAudio(
  snap: Pick<QueueSnapshot, "output_pref" | "renderer_kind" | "renderer_id"> | null | undefined,
  tabRendererId: string | null | undefined
): boolean {
  if (!snap) return false;
  if (snap.output_pref === "discord" || snap.renderer_kind === "discord") return true;
  if (tabRendererId && snap.renderer_id && snap.renderer_id !== tabRendererId) return true;
  return false;
}

export function outputPrefOf(snap: QueueSnapshot | null | undefined, fallback: "browser" | "discord" = "browser"): "browser" | "discord" {
  if (snap?.output_pref === "discord" || snap?.output_pref === "browser") return snap.output_pref;
  return fallback;
}

function mergeQueue(prev: QueueSnapshot, snap: QueueSnapshot): QueueSnapshot {
  return {
    ...prev,
    ...snap,
    items: Array.isArray(snap.items) ? snap.items : prev.items,
    volume: snap.volume ?? prev.volume,
    muted: snap.muted ?? prev.muted
  };
}

function playheadFromSnap(snap: QueueSnapshot, offsetMs: number, nowMs: number, prev: PlayheadView): PlayheadView {
  const checkpointAtMs = parseTimeMs(snap.checkpoint_at) ?? nowMs;
  const checkpointPositionMs = num(snap.position_ms, prev.checkpointPositionMs);
  const playbackRate = snap.playback_rate && snap.playback_rate > 0 ? snap.playback_rate : prev.playbackRate || 1;
  const durationMs = num(snap.duration_ms, prev.durationMs);
  const playing = snap.status === "playing";
  const positionMs = interpolatePosition({
    playing,
    checkpointPositionMs,
    checkpointAtMs,
    playbackRate,
    durationMs,
    nowMs,
    offsetMs
  });
  return {
    positionMs,
    checkpointPositionMs,
    checkpointAtMs,
    playbackRate,
    durationMs,
    playing,
    sequence: num(snap.playhead_sequence, prev.sequence),
    instanceId: snap.playback_instance_id ?? prev.instanceId,
    offsetMs,
    stateRevision: num(snap.state_revision, prev.stateRevision)
  };
}

export function applyBindResult(view: SessionView, result: BindResult, guildId?: string | null): SessionView {
  const rev = num(result.binding_revision, -1);
  const guild = guildId || result.guild_id || "";
  const last = guild ? view.lastBindingByGuild[guild] ?? view.lastBindingRevision : view.lastBindingRevision;
  if (rev >= 0 && rev < last) {
    return { ...view, ignored: "stale_bind", stateApplied: false, playheadApplied: false, stopAudio: false };
  }
  const lastBindingByGuild = guild && rev >= 0 ? { ...view.lastBindingByGuild, [guild]: rev } : view.lastBindingByGuild;
  return {
    ...view,
    ignored: null,
    lastBindingRevision: rev >= 0 ? Math.max(view.lastBindingRevision, rev) : view.lastBindingRevision,
    lastBindingByGuild,
    queue: {
      ...view.queue,
      binding_revision: rev >= 0 ? rev : view.queue.binding_revision,
      output_pref: "discord",
      playback_instance_id: result.playback_instance_id ?? view.queue.playback_instance_id,
      state_revision: result.state_revision ?? view.queue.state_revision
    },
    stateApplied: true,
    playheadApplied: false,
    stopAudio: true
  };
}

export function applyJoinFailure(view: SessionView): SessionView {
  return {
    ...view,
    ignored: null,
    queue: { ...view.queue, output_pref: "browser" },
    stopAudio: false,
    stateApplied: true,
    playheadApplied: false
  };
}

export function applySwitchToBrowser(view: SessionView, snap: QueueSnapshot, nowMs = Date.now()): SessionView {
  const next = applySnapshot(view, { ...snap, output_pref: "browser" }, { nowMs, kind: "snapshot" });
  return {
    ...next,
    queue: { ...next.queue, output_pref: "browser" },
    lastBindingRevision: view.lastBindingRevision,
    lastBindingByGuild: view.lastBindingByGuild,
    stopAudio: false
  };
}

export function applySnapshot(view: SessionView, snap: QueueSnapshot, opts: ApplyOpts = {}): SessionView {
  const nowMs = opts.nowMs ?? Date.now();
  const offsetMs = opts.clock?.offsetMs ?? view.clockOffsetMs;
  const kind = opts.kind || "snapshot";
  let ignored: SessionView["ignored"] = null;
  let stateApplied = false;
  let playheadApplied = false;
  let queue = view.queue;
  let lastAppliedRevision = view.lastAppliedRevision;
  let lastPlayheadSequence = view.lastPlayheadSequence;
  let lastInstanceId = view.lastInstanceId;
  let playhead = view.playhead;
  let lastBindingRevision = view.lastBindingRevision;
  let lastBindingByGuild = view.lastBindingByGuild;

  if (kind === "bind") {
    const rev = num(snap.binding_revision, -1);
    const guild = opts.guildId || "";
    const last = guild ? lastBindingByGuild[guild] ?? lastBindingRevision : lastBindingRevision;
    if (rev >= 0 && rev < last) {
      return { ...view, ignored: "stale_bind", stateApplied: false, playheadApplied: false, stopAudio: false };
    }
    if (rev >= 0) {
      lastBindingRevision = Math.max(lastBindingRevision, rev);
      if (guild) lastBindingByGuild = { ...lastBindingByGuild, [guild]: rev };
    }
  } else if (snap.binding_revision != null && Number.isFinite(snap.binding_revision)) {
    const rev = num(snap.binding_revision);
    lastBindingRevision = Math.max(lastBindingRevision, rev);
    if (opts.guildId) lastBindingByGuild = { ...lastBindingByGuild, [opts.guildId]: Math.max(lastBindingByGuild[opts.guildId] ?? 0, rev) };
  }

  const incomingRev = snap.state_revision;
  const hasRev = incomingRev != null && Number.isFinite(incomingRev);
  const staleState = kind !== "playhead" && hasRev && incomingRev < lastAppliedRevision;
  if (staleState) {
    ignored = "stale_revision";
  } else if (kind !== "playhead") {
    queue = mergeQueue(view.queue, snap);
    if (hasRev) lastAppliedRevision = incomingRev;
    stateApplied = true;
  }

  const instanceId = snap.playback_instance_id ?? null;
  const seq = snap.playhead_sequence;
  const hasSeq = seq != null && Number.isFinite(seq);
  const instanceChanged = instanceId != null && instanceId !== lastInstanceId && lastInstanceId != null;
  const sameInstance = instanceId != null && instanceId === lastInstanceId;
  const stalePlayhead = sameInstance && hasSeq && seq < lastPlayheadSequence && !instanceChanged;

  if (kind !== "bind" && (hasSeq || snap.position_ms != null || snap.checkpoint_at != null || snap.status != null)) {
    if (stalePlayhead) {
      if (!ignored) ignored = "stale_playhead";
    } else {
      const nextPlayhead = playheadFromSnap(
        stateApplied ? queue : { ...view.queue, ...snap, items: view.queue.items },
        offsetMs,
        nowMs,
        view.playhead
      );
      const revisionIncreased = hasRev && incomingRev > view.lastAppliedRevision;
      const prevPos = interpolatePosition({
        playing: view.playhead.playing,
        checkpointPositionMs: view.playhead.checkpointPositionMs,
        checkpointAtMs: view.playhead.checkpointAtMs,
        playbackRate: view.playhead.playbackRate,
        durationMs: view.playhead.durationMs,
        nowMs,
        offsetMs: view.playhead.offsetMs
      });
      if (
        view.playhead.sequence > 0 &&
        !revisionIncreased &&
        !instanceChanged &&
        lastInstanceId != null &&
        !shouldAcceptRefine(prevPos, nextPlayhead.positionMs, false)
      ) {
        playhead = { ...view.playhead, offsetMs };
      } else {
        playhead = nextPlayhead;
        playheadApplied = true;
        if (instanceId != null) lastInstanceId = instanceId;
        if (hasSeq) lastPlayheadSequence = seq;
        if (!stateApplied) {
          queue = {
            ...queue,
            position_ms: snap.position_ms ?? queue.position_ms,
            checkpoint_at: snap.checkpoint_at ?? queue.checkpoint_at,
            playhead_sequence: snap.playhead_sequence ?? queue.playhead_sequence,
            playback_instance_id: snap.playback_instance_id ?? queue.playback_instance_id,
            duration_ms: snap.duration_ms ?? queue.duration_ms,
            playback_rate: snap.playback_rate ?? queue.playback_rate,
            status: snap.status ?? queue.status
          };
        }
      }
    }
  }

  const appliedQueue = queue;
  const stopAudio = shouldStopHtmlAudio(appliedQueue, opts.tabRendererId);

  return {
    lastAppliedRevision,
    lastPlayheadSequence,
    lastInstanceId,
    lastBindingRevision,
    lastBindingByGuild,
    queue: appliedQueue,
    playhead,
    clockOffsetMs: offsetMs,
    ignored,
    stopAudio,
    stateApplied,
    playheadApplied
  };
}
