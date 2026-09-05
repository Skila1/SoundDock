import { describe, expect, it } from "vitest";
import {
  clockOffset,
  interpolatePosition,
  MAX_REFINE_JUMP_MS,
  parseTimeMs,
  sampleClock,
  shouldAcceptRefine
} from "@/stores/playhead";

describe("playhead interpolation", () => {
  it("advances from checkpoint while playing", () => {
    expect(
      interpolatePosition({
        playing: true,
        checkpointPositionMs: 1000,
        checkpointAtMs: 5_000,
        playbackRate: 1,
        durationMs: 10_000,
        nowMs: 8_000,
        offsetMs: 0
      })
    ).toBe(4_000);
  });

  it("holds checkpoint while paused", () => {
    expect(
      interpolatePosition({
        playing: false,
        checkpointPositionMs: 1000,
        checkpointAtMs: 5_000,
        playbackRate: 1,
        durationMs: 10_000,
        nowMs: 8_000,
        offsetMs: 0
      })
    ).toBe(1_000);
  });

  it("applies playback rate and clamps to duration", () => {
    expect(
      interpolatePosition({
        playing: true,
        checkpointPositionMs: 0,
        checkpointAtMs: 0,
        playbackRate: 2,
        durationMs: 3_000,
        nowMs: 4_000,
        offsetMs: 0
      })
    ).toBe(3_000);
  });

  it("adds RTT-aware clock offset to elapsed time", () => {
    expect(
      interpolatePosition({
        playing: true,
        checkpointPositionMs: 500,
        checkpointAtMs: 1_000,
        playbackRate: 1,
        durationMs: 20_000,
        nowMs: 1_500,
        offsetMs: 200
      })
    ).toBe(1_200);
  });
});

describe("parseTimeMs", () => {
  it("rejects zero and Go zero-time checkpoints so the playhead does not clamp to the end", () => {
    expect(parseTimeMs(0)).toBeNull();
    expect(parseTimeMs("0001-01-01T00:00:00Z")).toBeNull();
    expect(parseTimeMs("")).toBeNull();
    expect(parseTimeMs("1970-01-01T00:00:01.000Z")).toBe(1000);
  });

  it("parses RFC3339 and unix seconds as wall-clock milliseconds", () => {
    expect(parseTimeMs("2026-09-05T10:00:00.000Z")).toBe(Date.parse("2026-09-05T10:00:00.000Z"));
    expect(parseTimeMs(1_778_000_000)).toBe(1_778_000_000_000);
  });
});

describe("clock offset midpoint", () => {
  it("is server_time minus midpoint of local send and receive", () => {
    expect(clockOffset(2_000, 1_000, 1_200)).toBe(900);
    expect(sampleClock(900, 1_100, 1_000).offsetMs).toBe(0);
  });

  it("is zero when the response has no server_time", () => {
    expect(sampleClock(1_000, 1_080, null).offsetMs).toBe(0);
  });
});

describe("playhead refine slew", () => {
  it("rejects a jump over 1s unless state_revision increased", () => {
    expect(shouldAcceptRefine(5_000, 5_000 + MAX_REFINE_JUMP_MS + 1, false)).toBe(false);
    expect(shouldAcceptRefine(5_000, 8_000, true)).toBe(true);
    expect(shouldAcceptRefine(5_000, 5_400, false)).toBe(true);
  });
});
