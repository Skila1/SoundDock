import { api } from "@/lib/api";
import { getDeviceId, getTabRendererId } from "@/lib/device";
import type { QueueSnapshot } from "@/stores/sessionReducer";

/** Same-origin EventSource paths. W3-http may register either. */
export const QUEUE_SSE_PATHS = ["/api/v1/me/queue/sse", "/api/v1/me/queue/events"] as const;

/** Client presence ping. Independent of SSE `: ping` comments. */
export const HEARTBEAT_INTERVAL_MS = 15_000;

export type PresenceSource = "web" | "discord" | "both";

export type PresenceParticipant = {
  user_id: string;
  display_name: string;
  avatar_url: string | null;
  source: PresenceSource;
};

export type SessionStateEvent = QueueSnapshot & {
  queue?: QueueSnapshot;
  session?: QueueSnapshot;
};

export type SessionPlayheadEvent = {
  playback_instance_id?: string | null;
  checkpoint_position_ms?: number;
  position_ms?: number;
  checkpoint_at?: string | number | null;
  status?: string;
  playhead_sequence?: number;
  playback_rate?: number;
  duration_ms?: number;
  server_time?: string | number | null;
  state_revision?: number;
  binding_revision?: number | null;
};

export type SessionPresenceEvent = {
  listeners?: unknown;
  presence?: unknown;
  participants?: unknown;
  added?: unknown;
  removed?: unknown;
  left?: unknown;
};

export type AcquisitionStatusEvent = {
  intent_id?: string;
  job_id?: string;
  status?: string;
  correlation_id?: string;
  [key: string]: unknown;
};

export type ResourceInvalidateEvent = {
  scope?: string;
  keys?: string[];
  ids?: string[];
  resync?: boolean;
};

export type JobProgressEvent = {
  job_id?: string;
  progress?: number;
  keys?: string[];
};

export type SnapshotResult = {
  queue: QueueSnapshot;
  listeners?: PresenceParticipant[];
};

export type QueueSseHandlers = {
  fetchSnapshot: () => Promise<SnapshotResult | null>;
  onSnapshot: (result: SnapshotResult) => void | Promise<void>;
  onState: (snap: QueueSnapshot) => void;
  onPlayhead: (event: SessionPlayheadEvent) => void;
  onPresence: (event: SessionPresenceEvent) => void;
  onAcquisition?: (event: AcquisitionStatusEvent) => void;
  onInvalidate?: (event: ResourceInvalidateEvent) => void;
  onJobProgress?: (event: JobProgressEvent) => void;
  onAuthLost?: () => void;
};

function isRecord(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v);
}

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}

function idStr(v: unknown): string {
  if (typeof v === "string" && v.trim()) return v.trim();
  if (typeof v === "number" && Number.isFinite(v)) return String(v);
  return "";
}

function presenceSource(v: unknown): PresenceSource {
  const s = str(v).toLowerCase();
  if (s === "discord") return "discord";
  if (s === "both" || s === "web+discord" || s === "web_discord") return "both";
  return "web";
}

export function normalizeParticipant(raw: unknown): PresenceParticipant | null {
  if (!isRecord(raw)) return null;
  const user_id = idStr(raw.user_id) || idStr(raw.id) || idStr(raw.userId);
  const display_name =
    str(raw.display_name) || str(raw.display) || str(raw.name) || str(raw.username) || "Listener";
  const avatar =
    str(raw.avatar_url) || str(raw.avatar) || str(raw.avatarUrl) || str(raw.avatar_url_https);
  if (!user_id && display_name === "Listener") return null;
  return {
    user_id: user_id || display_name,
    display_name,
    avatar_url: avatar || null,
    source: presenceSource(raw.source)
  };
}

export function normalizePresenceList(raw: unknown): PresenceParticipant[] {
  const list = Array.isArray(raw)
    ? raw
    : isRecord(raw) && Array.isArray(raw.listeners)
      ? raw.listeners
      : isRecord(raw) && Array.isArray(raw.participants)
        ? raw.participants
        : isRecord(raw) && Array.isArray(raw.users)
          ? raw.users
          : [];
  const out: PresenceParticipant[] = [];
  const seen = new Set<string>();
  for (const item of list) {
    const p = normalizeParticipant(item);
    if (!p) continue;
    const key = p.user_id || p.display_name;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(p);
  }
  return out;
}

/** Pull GET/SSE snapshot listeners. Undefined if the payload has no presence field. */
export function pickListeners(snap: unknown): PresenceParticipant[] | undefined {
  if (!isRecord(snap)) return undefined;
  if ("listeners" in snap) return normalizePresenceList(snap.listeners);
  if ("presence" in snap) return normalizePresenceList(snap.presence);
  if ("participants" in snap) return normalizePresenceList(snap.participants);
  return undefined;
}

export function mergePresence(prev: PresenceParticipant[], event: SessionPresenceEvent): PresenceParticipant[] {
  const snapshot = pickListeners(event);
  if (snapshot && !event.added && !event.removed && !event.left) return snapshot;
  if (!event.added && !event.removed && !event.left) {
    if ("listeners" in event || "presence" in event || "participants" in event) {
      return snapshot ?? prev;
    }
    const one = normalizeParticipant(event);
    if (one) return [one];
  }

  const addedRaw = event.added;
  const removedRaw = event.removed ?? event.left;
  if (!addedRaw && !removedRaw) return snapshot ?? prev;

  const byId = new Map(prev.map((p) => [p.user_id, p]));
  const removed = Array.isArray(removedRaw) ? removedRaw : [];
  for (const item of removed) {
    if (typeof item === "string") byId.delete(item);
    else {
      const p = normalizeParticipant(item);
      if (p) byId.delete(p.user_id);
    }
  }
  const added = Array.isArray(addedRaw) ? addedRaw : [];
  for (const item of added) {
    const p = normalizeParticipant(item);
    if (p) byId.set(p.user_id, p);
  }
  return [...byId.values()];
}

export function unwrapStateSnapshot(payload: unknown): QueueSnapshot | null {
  if (!isRecord(payload)) return null;
  if (isRecord(payload.queue)) return payload.queue as QueueSnapshot;
  if (isRecord(payload.session)) return payload.session as QueueSnapshot;
  return payload as QueueSnapshot;
}

function timeField(v: unknown, fallback?: string | null): string | null | undefined {
  if (typeof v === "string") return v;
  if (typeof v === "number" && Number.isFinite(v)) {
    const ms = v > 0 && v < 1e12 ? v * 1000 : v;
    return new Date(ms).toISOString();
  }
  if (v == null) return fallback;
  return fallback;
}

export function playheadEventToSnap(event: SessionPlayheadEvent, prev: QueueSnapshot): QueueSnapshot {
  return {
    ...prev,
    position_ms: event.checkpoint_position_ms ?? event.position_ms ?? prev.position_ms,
    checkpoint_at: timeField(event.checkpoint_at, prev.checkpoint_at),
    playback_instance_id: event.playback_instance_id !== undefined ? event.playback_instance_id : prev.playback_instance_id,
    playhead_sequence: event.playhead_sequence ?? prev.playhead_sequence,
    playback_rate: event.playback_rate ?? prev.playback_rate,
    duration_ms: event.duration_ms ?? prev.duration_ms,
    status: event.status ?? prev.status,
    state_revision: event.state_revision ?? prev.state_revision,
    binding_revision: event.binding_revision !== undefined ? event.binding_revision : prev.binding_revision,
    server_time: timeField(event.server_time, prev.server_time)
  };
}

function parseJson(data: string): unknown {
  if (!data) return null;
  try {
    return JSON.parse(data);
  } catch {
    return null;
  }
}

function isAuthError(err: unknown): boolean {
  return typeof err === "object" && err !== null && (err as { status?: number }).status === 401;
}

export type QueueSseClient = {
  start: (opts?: { resync?: boolean }) => void;
  stop: () => void;
  get connected(): boolean;
};

export function createQueueSseClient(handlers: QueueSseHandlers): QueueSseClient {
  let es: EventSource | null = null;
  let stopped = true;
  let epoch = 0;
  let pathIndex = 0;
  let opened = false;
  let reconnectTimer: number | undefined;
  let heartbeatTimer: number | undefined;
  let backoffMs = 0;
  let started = false;

  function clearReconnect() {
    if (reconnectTimer !== undefined) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = undefined;
    }
  }

  function closeEs() {
    if (es) {
      es.onerror = null;
      es.onopen = null;
      es.close();
      es = null;
    }
  }

  function stopHeartbeat() {
    if (heartbeatTimer !== undefined) {
      window.clearInterval(heartbeatTimer);
      heartbeatTimer = undefined;
    }
  }

  async function postHeartbeat() {
    if (stopped) return;
    try {
      await api.post("/api/v1/me/queue/heartbeat", {
        client_id: getTabRendererId(),
        device_id: getDeviceId()
      });
    } catch (err) {
      if (isAuthError(err)) {
        handlers.onAuthLost?.();
        stop();
      }
    }
  }

  function startHeartbeat() {
    stopHeartbeat();
    void postHeartbeat();
    heartbeatTimer = window.setInterval(() => {
      void postHeartbeat();
    }, HEARTBEAT_INTERVAL_MS);
  }

  function scheduleReconnect() {
    if (stopped) return;
    clearReconnect();
    const delay = backoffMs;
    backoffMs = Math.min(8_000, Math.max(1_000, (backoffMs || 500) * 2));
    reconnectTimer = window.setTimeout(() => {
      void resyncAndSubscribe();
    }, delay);
  }

  function bindEvent(source: EventSource, name: string, fn: (ev: MessageEvent) => void) {
    source.addEventListener(name, fn as EventListener);
  }

  function connect() {
    if (stopped || typeof EventSource === "undefined") return;
    closeEs();
    opened = false;
    const path = QUEUE_SSE_PATHS[Math.min(pathIndex, QUEUE_SSE_PATHS.length - 1)];
    const params = new URLSearchParams();
    const clientId = getTabRendererId();
    const deviceId = getDeviceId();
    if (clientId) params.set("client_id", clientId);
    if (deviceId) params.set("device_id", deviceId);
    const qs = params.toString();
    const url = qs ? `${path}?${qs}` : path;
    const source = new EventSource(url, { withCredentials: true });
    es = source;

    source.onopen = () => {
      opened = true;
      backoffMs = 0;
    };

    bindEvent(source, "session.state", (ev) => {
      const snap = unwrapStateSnapshot(parseJson(ev.data));
      if (snap) handlers.onState(snap);
    });
    bindEvent(source, "session.playhead", (ev) => {
      const data = parseJson(ev.data);
      if (isRecord(data)) handlers.onPlayhead(data as SessionPlayheadEvent);
    });
    bindEvent(source, "session.presence", (ev) => {
      const data = parseJson(ev.data);
      if (Array.isArray(data)) {
        handlers.onPresence({ listeners: data });
        return;
      }
      if (isRecord(data)) handlers.onPresence(data as SessionPresenceEvent);
    });
    bindEvent(source, "acquisition.status", (ev) => {
      const data = parseJson(ev.data);
      if (isRecord(data)) handlers.onAcquisition?.(data as AcquisitionStatusEvent);
    });
    bindEvent(source, "resource.invalidate", (ev) => {
      const data = parseJson(ev.data);
      if (isRecord(data)) handlers.onInvalidate?.(data as ResourceInvalidateEvent);
    });
    bindEvent(source, "job.progress", (ev) => {
      const data = parseJson(ev.data);
      if (isRecord(data)) handlers.onJobProgress?.(data as JobProgressEvent);
    });
    // Named ping if the server uses it; comment `: ping` is not visible to EventSource.
    bindEvent(source, "ping", () => undefined);

    source.onerror = () => {
      const hadOpen = opened;
      closeEs();
      if (stopped) return;
      if (!hadOpen && pathIndex < QUEUE_SSE_PATHS.length - 1) {
        pathIndex += 1;
        connect();
        return;
      }
      scheduleReconnect();
    };
  }

  async function resyncAndSubscribe() {
    if (stopped) return;
    const my = ++epoch;
    pathIndex = 0;
    closeEs();
    try {
      const snap = await handlers.fetchSnapshot();
      if (stopped || my !== epoch) return;
      if (snap) await handlers.onSnapshot(snap);
    } catch (err) {
      if (isAuthError(err)) {
        handlers.onAuthLost?.();
        stop();
        return;
      }
    }
    if (stopped || my !== epoch) return;
    connect();
  }

  function onVisibility() {
    if (stopped) return;
    if (document.visibilityState === "visible") {
      void postHeartbeat();
      if (!es || es.readyState === EventSource.CLOSED) void resyncAndSubscribe();
    }
  }

  function start(opts?: { resync?: boolean }) {
    stopped = false;
    if (!started) {
      started = true;
      document.addEventListener("visibilitychange", onVisibility);
      window.addEventListener("pagehide", onPageHide);
    }
    startHeartbeat();
    if (es && (es.readyState === EventSource.OPEN || es.readyState === EventSource.CONNECTING)) return;
    if (opts?.resync === false) {
      connect();
      return;
    }
    void resyncAndSubscribe();
  }

  function onPageHide() {
    closeEs();
  }

  function stop() {
    stopped = true;
    epoch += 1;
    opened = false;
    clearReconnect();
    stopHeartbeat();
    closeEs();
    if (started) {
      started = false;
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", onPageHide);
    }
  }

  return {
    start,
    stop,
    get connected() {
      return !!es && es.readyState === EventSource.OPEN;
    }
  };
}
