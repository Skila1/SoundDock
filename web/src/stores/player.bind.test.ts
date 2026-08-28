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

type QueueItem = QueueState["items"][number] & { media_state?: string; origin?: string };

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
    items: [{ id: "i1", position: 0, track_id: "t1" }],
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

function resetStore(q: QueueState | null = null) {
  resetPlayerSessionForTests(
    q
      ? {
          ...initialSession(),
          lastAppliedRevision: q.state_revision || 0,
          lastPlayheadSequence: q.playhead_sequence || 0,
          lastInstanceId: q.playback_instance_id || null,
          lastBindingRevision: q.binding_revision || 0,
          queue: q
        }
      : undefined
  );
  usePlayer.setState({
    queue: q,
    playing: !!q && q.status === "playing",
    output: "browser",
    voice: null,
    current: q?.current_track_id ? { id: q.current_track_id, title: "Song" } : undefined,
    position: q?.position_ms || 0,
    duration: q?.duration_ms || 0
  });
}

describe("bindTrack gating", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    sessionStorage.clear();
    localStorage.clear();
    resetStore(null);
    vi.mocked(api.get).mockReset();
    vi.mocked(api.post).mockReset();
    vi.mocked(api.put).mockReset();
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
    vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("renderer/acquire")) {
        return queue({ renderer_id: getTabRendererId(), renderer_kind: "browser", output_pref: "browser" });
      }
      return queue();
    });
    vi.mocked(api.get).mockResolvedValue({ id: "t1", title: "Song" });
  });

  it("does not call bindTrack when current item is restoring", async () => {
    const bind = vi.spyOn(engine, "bindTrack");
    const restoring = queue({
      current_track_id: "yt1",
      items: [{ id: "i1", position: 0, track_id: "yt1", media_state: "restoring" }]
    });
    vi.mocked(api.put).mockResolvedValue(restoring);
    vi.mocked(api.get).mockResolvedValue({ id: "yt1", title: "Song" });

    await usePlayer.getState().playTracks(["yt1"]);

    expect(bind).not.toHaveBeenCalled();
    expect(usePlayer.getState().queue?.items?.[0]).toMatchObject({ media_state: "restoring", track_id: "yt1" });
  });

  it("calls bindTrack when current item is ready", async () => {
    const bind = vi.spyOn(engine, "bindTrack");
    vi.mocked(api.put).mockResolvedValue(
      queue({
        items: [{ id: "i1", position: 0, track_id: "t1", media_state: "ready" }]
      })
    );

    await usePlayer.getState().playTracks(["t1"]);

    expect(bind).toHaveBeenCalled();
    expect(bind.mock.calls.some((c) => c[1] === "t1")).toBe(true);
  });

  it("calls bindTrack when media_state is absent", async () => {
    const bind = vi.spyOn(engine, "bindTrack");
    vi.mocked(api.put).mockResolvedValue(queue());

    await usePlayer.getState().playTracks(["t1"]);

    expect(bind).toHaveBeenCalled();
    expect(bind.mock.calls.some((c) => c[1] === "t1")).toBe(true);
  });

  it("binds after a later snapshot marks the same current id ready", async () => {
    const restoring = queue({
      items: [{ id: "i1", position: 0, track_id: "t1", media_state: "restoring" }]
    });
    resetStore(restoring);
    const bind = vi.spyOn(engine, "bindTrack");

    applyRemoteQueueForTests(
      queue({
        state_revision: 5,
        items: [{ id: "i1", position: 0, track_id: "t1", media_state: "ready" }]
      })
    );

    await vi.waitFor(() => {
      expect(bind).toHaveBeenCalled();
    });
    expect(bind.mock.calls.some((c) => c[1] === "t1")).toBe(true);
  });

  it("does not bind when snapshot stays restoring", async () => {
    const restoring = queue({
      items: [{ id: "i1", position: 0, track_id: "t1", media_state: "restoring" }]
    });
    resetStore(restoring);
    const bind = vi.spyOn(engine, "bindTrack");

    applyRemoteQueueForTests(
      queue({
        state_revision: 5,
        items: [{ id: "i1", position: 0, track_id: "t1", media_state: "restoring", origin: "youtube:abc" }]
      })
    );

    await Promise.resolve();
    await Promise.resolve();
    expect(bind).not.toHaveBeenCalled();
  });
});
