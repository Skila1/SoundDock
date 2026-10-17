import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  REMOTE_DEDUPE_MS,
  acceptRemote,
  attachMediaRemote,
  bindMediaSession,
  claimPlaybackSession,
  clearMediaSession,
  mediaArtwork,
  mediaKeyAction,
  positionChanged,
  resetRemoteForTests,
  runRemote,
  updateMediaPosition
} from "@/stores/mediaSession";

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

function remoteHandlers() {
  return {
    play: vi.fn(),
    pause: vi.fn(),
    next: vi.fn(),
    previous: vi.fn(),
    stop: vi.fn(),
    seekTo: vi.fn(),
    seekBy: vi.fn(),
    toggle: vi.fn()
  };
}

describe("attachMediaRemote", () => {
  beforeEach(() => resetRemoteForTests());

  it("registers transport, stop and seek actions on desktop", () => {
    const { handlers } = mockMediaSession();
    const h = remoteHandlers();
    attachMediaRemote(h);
    for (const name of ["play", "pause", "nexttrack", "previoustrack", "stop", "seekto", "seekforward", "seekbackward"]) {
      expect(handlers.get(name), name).toBeTruthy();
    }
    handlers.get("nexttrack")?.({ action: "nexttrack" });
    handlers.get("previoustrack")?.({ action: "previoustrack" });
    handlers.get("stop")?.({ action: "stop" });
    handlers.get("seekto")?.({ action: "seekto", seekTime: 42 });
    handlers.get("seekforward")?.({ action: "seekforward" });
    handlers.get("seekbackward")?.({ action: "seekbackward", seekOffset: 5 });
    expect(h.next).toHaveBeenCalled();
    expect(h.previous).toHaveBeenCalled();
    expect(h.stop).toHaveBeenCalled();
    expect(h.seekTo).toHaveBeenCalledWith(42000);
    expect(h.seekBy).toHaveBeenCalledWith(10000);
    expect(h.seekBy).toHaveBeenCalledWith(-5000);
  });

  it("keeps lock-screen track buttons on iOS by clearing seek actions", () => {
    vi.spyOn(navigator, "userAgent", "get").mockReturnValue("Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)");
    const { handlers } = mockMediaSession();
    attachMediaRemote(remoteHandlers());
    expect(handlers.get("nexttrack")).toBeTruthy();
    expect(handlers.get("seekforward")).toBeNull();
    expect(handlers.get("seekbackward")).toBeNull();
    vi.restoreAllMocks();
  });

  it("survives engines that reject unknown actions", () => {
    const mediaSession = {
      metadata: null,
      playbackState: "none",
      setActionHandler: (name: string) => {
        if (name === "stop" || name.startsWith("seek")) throw new TypeError("unsupported");
      }
    };
    Object.defineProperty(navigator, "mediaSession", { configurable: true, value: mediaSession });
    expect(() => attachMediaRemote(remoteHandlers())).not.toThrow();
  });
});

describe("media keys", () => {
  beforeEach(() => resetRemoteForTests());

  it("maps the Windows media keys (VK 176, 177, 179) to commands", () => {
    expect(mediaKeyAction({ key: "MediaPlayPause", code: "MediaPlayPause" })).toBe("toggle");
    expect(mediaKeyAction({ key: "MediaTrackNext", code: "MediaTrackNext" })).toBe("next");
    expect(mediaKeyAction({ key: "MediaTrackPrevious", code: "MediaTrackPrevious" })).toBe("previous");
    expect(mediaKeyAction({ key: "MediaStop", code: "MediaStop" })).toBe("stop");
    expect(mediaKeyAction({ key: "Unidentified", code: "MediaTrackNext" })).toBe("next");
    expect(mediaKeyAction({ key: " ", code: "Space" })).toBeNull();
  });

  it("runs a key once when both the keydown and Media Session receive it", () => {
    const h = remoteHandlers();
    expect(runRemote("toggle", "keyboard", h)).toBe(true);
    expect(runRemote("pause", "mediasession", h)).toBe(false);
    expect(h.toggle).toHaveBeenCalledTimes(1);
    expect(h.pause).not.toHaveBeenCalled();
  });

  it("lets repeated presses from one source through", () => {
    const h = remoteHandlers();
    runRemote("next", "mediasession", h);
    runRemote("next", "mediasession", h);
    expect(h.next).toHaveBeenCalledTimes(2);
  });

  it("only dedupes inside the window and per command", () => {
    expect(acceptRemote("next", "keyboard", 1000)).toBe(true);
    expect(acceptRemote("previous", "mediasession", 1010)).toBe(true);
    expect(acceptRemote("next", "mediasession", 1000 + REMOTE_DEDUPE_MS + 1)).toBe(true);
  });
});

describe("position publishing", () => {
  beforeEach(() => resetRemoteForTests());

  it("publishes on state change or drift, not every tick", () => {
    const base = { duration: 200000, playbackRate: 1, position: 10000, playing: true, at: 0 };
    expect(positionChanged(null, base, 0)).toBe(true);
    expect(positionChanged(base, { ...base, position: 10250 }, 250)).toBe(false);
    expect(positionChanged(base, { ...base, position: 60000 }, 250)).toBe(true);
    expect(positionChanged(base, { ...base, playing: false }, 10)).toBe(true);
    expect(positionChanged(base, { ...base, playbackRate: 1.5 }, 10)).toBe(true);
  });

  it("keeps playbackState in sync and clamps position", () => {
    const { mediaSession } = mockMediaSession();
    updateMediaPosition({ duration: 100000, playbackRate: 1, position: 150000, playing: true });
    expect(mediaSession.playbackState).toBe("playing");
    expect(mediaSession.setPositionState).toHaveBeenCalledWith({ duration: 100, playbackRate: 1, position: 100 });
    updateMediaPosition({ duration: 100000, playbackRate: 1, position: 100000, playing: false });
    expect(mediaSession.playbackState).toBe("paused");
  });

  it("clears the OS overlay when nothing is loaded", () => {
    const { mediaSession } = mockMediaSession();
    bindMediaSession({ id: "t1", title: "Song" });
    clearMediaSession();
    expect(mediaSession.metadata).toBeNull();
    expect(mediaSession.playbackState).toBe("none");
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
