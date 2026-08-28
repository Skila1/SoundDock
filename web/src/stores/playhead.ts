export const MAX_REFINE_JUMP_MS = 1000;

export type ClockSample = {
  localSend: number;
  localReceive: number;
  serverTime: number | null;
  offsetMs: number;
};

export type PlayheadInput = {
  playing: boolean;
  checkpointPositionMs: number;
  checkpointAtMs: number;
  playbackRate?: number;
  durationMs?: number;
  nowMs: number;
  offsetMs: number;
};

export function nowMono(): number {
  if (typeof performance !== "undefined" && typeof performance.now === "function") {
    return performance.now();
  }
  return Date.now();
}

export function parseTimeMs(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value > 0 && value < 1e12 ? value * 1000 : value;
  }
  if (typeof value === "string" && value.trim()) {
    const parsed = Date.parse(value);
    if (Number.isFinite(parsed)) return parsed;
    const num = Number(value);
    if (Number.isFinite(num)) return num > 0 && num < 1e12 ? num * 1000 : num;
  }
  return null;
}

/** offset ≈ server_time - midpoint(local_send, local_receive) */
export function clockOffset(serverTime: number, localSend: number, localReceive: number): number {
  return serverTime - (localSend + localReceive) / 2;
}

export function sampleClock(localSend: number, localReceive: number, serverTime: number | null): ClockSample {
  const offsetMs = serverTime == null ? 0 : clockOffset(serverTime, localSend, localReceive);
  return { localSend, localReceive, serverTime, offsetMs };
}

export function clampPosition(pos: number, durationMs?: number): number {
  if (!Number.isFinite(pos) || pos < 0) return 0;
  if (durationMs != null && durationMs > 0 && pos > durationMs) return durationMs;
  return pos;
}

/**
 * if playing: pos = checkpoint_position_ms + rate * (now + offset - checkpoint_at)
 * else pos = checkpoint. Clamp to [0, duration].
 */
export function interpolatePosition(input: PlayheadInput): number {
  const rate = input.playbackRate && input.playbackRate > 0 ? input.playbackRate : 1;
  const pos = input.playing
    ? input.checkpointPositionMs + rate * (input.nowMs + input.offsetMs - input.checkpointAtMs)
    : input.checkpointPositionMs;
  return clampPosition(pos, input.durationMs);
}

export function shouldAcceptRefine(prevPos: number, nextPos: number, stateRevisionIncreased: boolean): boolean {
  if (stateRevisionIncreased) return true;
  return Math.abs(nextPos - prevPos) <= MAX_REFINE_JUMP_MS;
}
