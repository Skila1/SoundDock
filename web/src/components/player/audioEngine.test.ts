import { beforeEach, describe, expect, it, vi } from "vitest";
import { bindTrack, isMediaReady, preloadTrack } from "@/components/player/audioEngine";

vi.mock("@/lib/api", () => ({
  streamUrl: (id: string) => `/api/v1/tracks/${id}/stream?quality=original`
}));

vi.mock("@/offline", () => ({
  offlineObjectUrl: async () => null
}));

describe("isMediaReady", () => {
  it("treats absent media_state as ready", () => {
    expect(isMediaReady(undefined)).toBe(true);
    expect(isMediaReady(null)).toBe(true);
    expect(isMediaReady("")).toBe(true);
  });

  it("is ready only for explicit ready", () => {
    expect(isMediaReady("ready")).toBe(true);
    expect(isMediaReady("restoring")).toBe(false);
    expect(isMediaReady("missing_external")).toBe(false);
  });
});

describe("bindTrack media_state gate", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("does not set src when restoring", async () => {
    const el = document.createElement("audio");
    await bindTrack(el, "t1", "restoring");
    expect(el.getAttribute("src")).toBeFalsy();
    expect(el.dataset.trackId).toBeFalsy();
  });

  it("does not set src when missing_external", async () => {
    const el = document.createElement("audio");
    await bindTrack(el, "t1", "missing_external");
    expect(el.getAttribute("src")).toBeFalsy();
    expect(el.dataset.trackId).toBeFalsy();
  });

  it("sets src when ready", async () => {
    const el = document.createElement("audio");
    await bindTrack(el, "t1", "ready");
    expect(el.src).toContain("/api/v1/tracks/t1/stream");
    expect(el.dataset.trackId).toBe("t1");
  });

  it("sets src when media_state is absent", async () => {
    const el = document.createElement("audio");
    await bindTrack(el, "t1");
    expect(el.src).toContain("/api/v1/tracks/t1/stream");
    expect(el.dataset.trackId).toBe("t1");
  });

  it("clears a previously bound src when later restoring", async () => {
    const el = document.createElement("audio");
    await bindTrack(el, "t1", "ready");
    await bindTrack(el, "t1", "restoring");
    expect(el.getAttribute("src")).toBeFalsy();
    expect(el.dataset.trackId).toBeFalsy();
  });
});

describe("preloadTrack media_state gate", () => {
  it("does not bind the idle element when restoring", async () => {
    preloadTrack("next-1", "restoring");
    await Promise.resolve();
    const idle = document.querySelectorAll("audio");
    for (const el of idle) {
      if ((el as HTMLAudioElement).dataset.trackId === "next-1") {
        throw new Error("preload bound restoring track");
      }
    }
  });
});
