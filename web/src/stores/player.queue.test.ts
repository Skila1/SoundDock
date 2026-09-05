import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@/lib/api";
import { getTabRendererId } from "@/lib/device";
import { initialSession } from "@/stores/sessionReducer";
import { applyRemoteQueueForTests, resetPlayerSessionForTests, usePlayer } from "@/stores/player";
import * as engine from "@/components/player/audioEngine";
import type { QueueState } from "@/types/api";

vi.mock("@/lib/api", () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    del: vi.fn()
  },
  streamUrl: (id: string) => `/api/v1/tracks/${id}/stream?quality=original`
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), message: vi.fn() }
}));

type QueueItem = QueueState["items"][number] & { media_state?: string };

function queue(partial: Partial<QueueState> & { items?: QueueItem[] } = {}): QueueState {
  return {
    id: "sess-1",
    status: "playing",
    volume: 0.7,
    shuffle: false,
    repeat: "off",
    crossfade_seconds: 0,
    replaygain_mode: "off",
    current_index: 0,
    current_track_id: "t1",
    position_ms: 8_000,
    items: [{ id: "i1", position: 0, track_id: "t1", media_state: "ready" }],
    muted: false,
    output_pref: "browser",
    renderer_kind: "browser",
    renderer_id: getTabRendererId(),
    playback_instance_id: "inst-1",
    state_revision: 4,
    playhead_sequence: 4,
    binding_revision: 3,
    checkpoint_at: new Date().toISOString(),
    duration_ms: 180_000,
    playback_rate: 1,
    ...partial
  };
}

function seedPlaying(q: QueueState = queue()) {
  resetPlayerSessionForTests({
    ...initialSession(),
    lastAppliedRevision: q.state_revision || 0,
    lastPlayheadSequence: q.playhead_sequence || 0,
    lastInstanceId: q.playback_instance_id || null,
    lastBindingRevision: q.binding_revision || 0,
    queue: q
  });
  usePlayer.setState({
    queue: q,
    playing: true,
    output: "browser",
    voice: null,
    current: { id: "t1", title: "Song" },
    position: 12_000,
    duration: 180_000
  });
  return q;
}

describe("add to queue keeps playback", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    sessionStorage.clear();
    localStorage.clear();
    resetPlayerSessionForTests();
    vi.mocked(api.get).mockReset();
    vi.mocked(api.post).mockReset();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo) => {
        const url = String(input);
        if (url.includes("voice-state")) {
          return { ok: true, json: async () => ({ discord_enabled: false, linked: false, in_voice: false }) } as Response;
        }
        return { ok: false, json: async () => ({}) } as Response;
      })
    );
  });

  it("does not pause HTML audio when adding a later track", async () => {
    seedPlaying();
    const pause = vi.spyOn(engine, "pauseAll");
    const added = queue({
      state_revision: 5,
      items: [
        { id: "i1", position: 0, track_id: "t1", media_state: "ready" },
        { id: "i2", position: 1, track_id: "t2", media_state: "ready" }
      ]
    });
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/me/queue/add")) return added;
      throw new Error(`unexpected POST ${url}`);
    });

    await usePlayer.getState().add(["t2"]);

    expect(pause).not.toHaveBeenCalled();
    expect(usePlayer.getState().playing).toBe(true);
    expect(usePlayer.getState().queue?.current_track_id).toBe("t1");
    expect(usePlayer.getState().queue?.items).toHaveLength(2);
    expect(usePlayer.getState().position).toBe(12_000);
  });

  it("does not pause when the add SSE echo arrives with the same current track", () => {
    seedPlaying();
    const pause = vi.spyOn(engine, "pauseAll");

    applyRemoteQueueForTests(
      queue({
        state_revision: 5,
        items: [
          { id: "i1", position: 0, track_id: "t1", media_state: "ready" },
          { id: "i2", position: 1, track_id: "t2", media_state: "ready" }
        ]
      })
    );

    expect(pause).not.toHaveBeenCalled();
    expect(usePlayer.getState().playing).toBe(true);
    expect(usePlayer.getState().position).toBe(12_000);
  });

  it("does not pause a local playhead when an add snapshot is still paused on the server", () => {
    const paused = queue({ status: "paused" });
    seedPlaying(paused);
    const pause = vi.spyOn(engine, "pauseAll");

    applyRemoteQueueForTests(
      queue({
        status: "paused",
        state_revision: 5,
        items: [
          { id: "i1", position: 0, track_id: "t1", media_state: "ready" },
          { id: "i2", position: 1, track_id: "t2", media_state: "ready" }
        ]
      })
    );

    expect(pause).not.toHaveBeenCalled();
    expect(usePlayer.getState().playing).toBe(true);
  });

  it("still pauses when a snapshot changes status from playing to paused", () => {
    seedPlaying();
    const pause = vi.spyOn(engine, "pauseAll");

    applyRemoteQueueForTests(queue({ status: "paused", state_revision: 5 }));

    expect(pause).toHaveBeenCalled();
    expect(usePlayer.getState().playing).toBe(false);
  });

  it("pauses when another tab pauses during the add window", async () => {
    seedPlaying();
    const pause = vi.spyOn(engine, "pauseAll");
    const added = queue({
      state_revision: 5,
      items: [
        { id: "i1", position: 0, track_id: "t1", media_state: "ready" },
        { id: "i2", position: 1, track_id: "t2", media_state: "ready" }
      ]
    });
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/me/queue/add")) return added;
      throw new Error(`unexpected POST ${url}`);
    });

    await usePlayer.getState().add(["t2"]);
    expect(usePlayer.getState().playing).toBe(true);
    expect(pause).not.toHaveBeenCalled();

    applyRemoteQueueForTests(
      queue({
        status: "paused",
        state_revision: 6,
        items: [
          { id: "i1", position: 0, track_id: "t1", media_state: "ready" },
          { id: "i2", position: 1, track_id: "t2", media_state: "ready" }
        ]
      })
    );

    expect(pause).toHaveBeenCalled();
    expect(usePlayer.getState().playing).toBe(false);
  });

  it("pause control sends position_ms", async () => {
    seedPlaying();
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/me/queue/control")) {
        return queue({ status: "paused", position_ms: 12_000, state_revision: 5 });
      }
      throw new Error(`unexpected POST ${url}`);
    });

    await usePlayer.getState().control("pause");

    const body = (vi.mocked(api.post).mock.calls[0]?.[1] || {}) as {
      extra?: { position_ms?: number };
    };
    expect(body.extra?.position_ms).toBe(12_000);
  });

  it("saveQueueAsPlaylist omits failed and cancelled items", async () => {
    resetPlayerSessionForTests();
    applyRemoteQueueForTests(queue({
      items: [
        { id: "i1", position: 0, track_id: "t1", media_state: "ready" },
        { id: "i2", position: 1, track_id: "t-fail", media_state: "failed" },
        { id: "i3", position: 2, track_id: "t-cancel", media_state: "cancelled" },
        { id: "i4", position: 3, track_id: "t-rest", media_state: "restoring" }
      ]
    }));
    vi.mocked(api.post).mockImplementation(async (url: string, body?: unknown) => {
      if (String(url) === "/api/v1/playlists") return { id: "pl1" };
      if (String(url).includes("/tracks")) {
        const ids = (body as { track_ids?: string[] }).track_ids || [];
        expect(ids).toEqual(["t1", "t-rest"]);
        return { added: ids.length };
      }
      throw new Error(`unexpected POST ${url}`);
    });
    await usePlayer.getState().saveQueueAsPlaylist("From queue");
    expect(api.post).toHaveBeenCalled();
  });
});

describe("autoplay persists on the session", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    sessionStorage.clear();
    localStorage.clear();
    resetPlayerSessionForTests();
    vi.mocked(api.get).mockReset();
    vi.mocked(api.post).mockReset();
  });

  it("writes autoplay to the session so Discord polls cannot flip the switch off", async () => {
    seedPlaying(queue({ autoplay: false }));
    usePlayer.setState({ autoplay: false });
    vi.mocked(api.post).mockImplementation(async (url: string, body?: unknown) => {
      if (!String(url).includes("/me/queue/control")) {
        throw new Error(`unexpected POST ${url}`);
      }
      const sent = body as { action?: string; extra?: { autoplay?: boolean } };
      expect(sent.action).toBe("autoplay");
      expect(sent.extra?.autoplay).toBe(true);
      return queue({ autoplay: true, state_revision: 6 });
    });

    await usePlayer.getState().setAutoplay(true);

    expect(usePlayer.getState().autoplay).toBe(true);
    applyRemoteQueueForTests(queue({ autoplay: true, state_revision: 7 }));
    expect(usePlayer.getState().autoplay).toBe(true);
  });

  it("follows the session when autoplay is turned off remotely", () => {
    seedPlaying(queue({ autoplay: true }));
    usePlayer.setState({ autoplay: true });
    applyRemoteQueueForTests(queue({ autoplay: false, state_revision: 8 }));
    expect(usePlayer.getState().autoplay).toBe(false);
  });
});
