import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { attachMediaRemote, bindMediaSession, claimPlaybackSession, mediaArtwork } from "@/stores/mediaSession";

beforeAll(() => {
  class FakeMediaMetadata {
    title = "";
    artist = "";
    album = "";
    artwork: MediaImage[] = [];
    constructor(init?: MediaMetadataInit) {
      this.title = init?.title || "";
      this.artist = init?.artist || "";
      this.album = init?.album || "";
      this.artwork = (init?.artwork || []) as MediaImage[];
    }
  }
  Object.defineProperty(globalThis, "MediaMetadata", { configurable: true, value: FakeMediaMetadata });
});

function mockMediaSession() {
  const handlers = new Map<string, MediaSessionActionHandler | null>();
  const mediaSession = {
    metadata: null as MediaMetadata | null,
    playbackState: "none",
    setActionHandler: (name: string, handler: MediaSessionActionHandler | null) => {
      handlers.set(name, handler);
    },
    setPositionState: vi.fn()
  };
  Object.defineProperty(navigator, "mediaSession", { configurable: true, value: mediaSession });
  return { mediaSession, handlers };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("mediaArtwork", () => {
  it("uses absolute artwork URLs for lock screens", () => {
    const art = mediaArtwork("track-1");
    expect(art.some((a) => a.sizes === "640x640")).toBe(true);
    expect(art[0].src).toContain("/api/v1/tracks/track-1/artwork");
  });
});

describe("bindMediaSession", () => {
  it("writes title, artists, and artwork", () => {
    const { mediaSession } = mockMediaSession();
    bindMediaSession({
      id: "t1",
      title: "Song",
      album: "LP",
      artists: [{ name: "A" }, { name: "B" }]
    });
    expect(mediaSession.metadata?.title).toBe("Song");
    expect(mediaSession.metadata?.artist).toBe("A, B");
    expect(mediaSession.metadata?.album).toBe("LP");
    expect((mediaSession.metadata?.artwork || []).length).toBeGreaterThan(0);
  });
});

describe("attachMediaRemote", () => {
  it("registers skip track actions and clears ±10s seek", () => {
    const { handlers } = mockMediaSession();
    const next = vi.fn();
    const previous = vi.fn();
    attachMediaRemote({
      play: vi.fn(),
      pause: vi.fn(),
      next,
      previous,
      seekTo: vi.fn()
    });
    expect(handlers.get("nexttrack")).toBeTruthy();
    expect(handlers.get("previoustrack")).toBeTruthy();
    expect(handlers.get("seekforward")).toBeNull();
    expect(handlers.get("seekbackward")).toBeNull();
    handlers.get("nexttrack")?.({ action: "nexttrack" });
    handlers.get("previoustrack")?.({ action: "previoustrack" });
    expect(next).toHaveBeenCalled();
    expect(previous).toHaveBeenCalled();
  });
});

describe("claimPlaybackSession", () => {
  it("sets Safari audioSession to playback", () => {
    const session = { type: "auto", state: "active", addEventListener: vi.fn() };
    Object.defineProperty(navigator, "audioSession", { configurable: true, value: session });
    const onResume = vi.fn();
    claimPlaybackSession(onResume);
    expect(session.type).toBe("playback");
    expect(session.addEventListener).toHaveBeenCalledWith("statechange", expect.any(Function));
  });
});
