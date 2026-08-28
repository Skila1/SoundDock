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

vi.mock("sonner", () => {
  const toast = Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn(), message: vi.fn() });
  return { toast };
});

type QueueItem = QueueState["items"][number] & { media_state?: string; origin?: string };

const nowPlaying: QueueItem = { id: "i1", position: 0, track_id: "t1" };
const upcoming: QueueItem = { id: "i2", position: 1, track_id: "t2" };

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
    items: [nowPlaying, upcoming],
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
    duration: q?.duration_ms || 0,
    pendingUndo: null
  });
}

function controlBody(call: unknown[]): { action?: string; extra?: Record<string, unknown>; command_id?: string } {
  return ((call?.[1] as { action?: string; extra?: Record<string, unknown>; command_id?: string }) || {});
}

describe("queue undo client", () => {
  const removed = { id: "i2", position: 1, track_id: "t2" };
  const afterRemove = {
    ...queue({
      state_revision: 5,
      items: [nowPlaying]
    }),
    undo_generation: 5,
    undo: { items: [removed] }
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    sessionStorage.clear();
    localStorage.clear();
    resetStore(queue());
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
    vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.spyOn(engine, "bindTrack").mockResolvedValue(undefined);
    vi.mocked(api.get).mockResolvedValue({ id: "t1", title: "Song" });
    vi.mocked(api.post).mockResolvedValue(afterRemove);
  });

  it("sets pending undo from a remove control result", async () => {
    await usePlayer.getState().control("remove", { position: 1 });

    expect(usePlayer.getState().pendingUndo).toEqual({
      undo_generation: 5,
      items: [expect.objectContaining({ id: "i2", track_id: "t2", position: 1 })]
    });
  });

  it("clears pending undo when state_revision moves past undo_generation", async () => {
    await usePlayer.getState().control("remove", { position: 1 });
    expect(usePlayer.getState().pendingUndo?.undo_generation).toBe(5);

    applyRemoteQueueForTests(
      queue({
        state_revision: 6,
        items: [nowPlaying]
      })
    );

    expect(usePlayer.getState().pendingUndo).toBeNull();
  });

  it("undo calls control with the stored generation and a new command_id", async () => {
    await usePlayer.getState().control("remove", { position: 1 });
    const removeCmd = controlBody(vi.mocked(api.post).mock.calls[0] || []).extra?.command_id;
    vi.mocked(api.post).mockResolvedValue(
      queue({
        state_revision: 6,
        items: [nowPlaying, upcoming]
      })
    );

    await usePlayer.getState().undo();

    const undoCall = vi.mocked(api.post).mock.calls.find((c) => controlBody(c).action === "undo");
    expect(undoCall).toBeTruthy();
    const body = controlBody(undoCall || []);
    expect(body.action).toBe("undo");
    expect(body.extra).toEqual(
      expect.objectContaining({
        undo_generation: 5,
        items: [expect.objectContaining({ id: "i2", track_id: "t2" })],
        command_id: expect.any(String)
      })
    );
    expect(body.extra?.command_id).toBeTruthy();
    expect(body.extra?.command_id).not.toBe(removeCmd);
    expect(usePlayer.getState().pendingUndo).toBeNull();
  });

  it("keeps pending undo when a snapshot stays on undo_generation", async () => {
    await usePlayer.getState().control("remove", { position: 1 });
    applyRemoteQueueForTests(
      queue({
        state_revision: 5,
        items: [nowPlaying]
      })
    );
    expect(usePlayer.getState().pendingUndo?.undo_generation).toBe(5);
  });

  it("drops pending undo and toasts on a 409 undo", async () => {
    const { toast } = await import("sonner");
    await usePlayer.getState().control("remove", { position: 1 });
    const stale = Object.assign(new Error("stale undo"), { status: 409 });
    vi.mocked(api.post).mockRejectedValue(stale);

    await usePlayer.getState().undo();

    expect(usePlayer.getState().pendingUndo).toBeNull();
    expect(toast.error).toHaveBeenCalledWith("Undo expired");
  });
});
