import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@/lib/api";
import { getTabRendererId } from "@/lib/device";
import { initialSession } from "@/stores/sessionReducer";
import { getPlayerSessionForTests, resetPlayerSessionForTests, usePlayer } from "@/stores/player";
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

const voice = {
  discord_enabled: true,
  linked: true,
  in_voice: true,
  guild_id: "g1",
  channel_id: "c1"
};

function queue(partial: Partial<QueueState> = {}): QueueState {
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

function seedBrowserPlaying() {
  const q = queue();
  resetPlayerSessionForTests({
    ...initialSession(),
    lastAppliedRevision: 4,
    lastPlayheadSequence: 4,
    lastInstanceId: "inst-1",
    lastBindingRevision: 3,
    lastBindingByGuild: { g1: 3 },
    queue: q
  });
  usePlayer.setState({
    queue: q,
    playing: true,
    output: "browser",
    voice,
    current: { id: "t1", title: "Song" },
    position: 8_000,
    duration: 180_000,
    volume: 0.7,
    muted: false
  });
  return q;
}

describe("output switch", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    sessionStorage.clear();
    localStorage.clear();
    resetPlayerSessionForTests();
    vi.mocked(api.get).mockReset();
    vi.mocked(api.post).mockReset();
    vi.mocked(api.put).mockReset();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo) => {
        const url = String(input);
        if (url.includes("voice-state")) {
          return { ok: true, json: async () => voice } as Response;
        }
        return { ok: false, json: async () => ({}) } as Response;
      })
    );
  });

  it("keeps browser output and lease when Discord join fails", async () => {
    seedBrowserPlaying();
    const pause = vi.spyOn(engine, "pauseAll");
    const play = vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/discord/join")) throw new Error("not in voice");
      if (String(url).includes("renderer/acquire")) {
        return queue({ renderer_id: getTabRendererId(), renderer_kind: "browser", output_pref: "browser" });
      }
      return queue();
    });

    await usePlayer.getState().setOutput("discord");

    expect(usePlayer.getState().output).toBe("browser");
    expect(getPlayerSessionForTests().queue.output_pref).toBe("browser");
    expect(pause).toHaveBeenCalled();
    expect(play).toHaveBeenCalled();
  });

  it("does not play HTMLAudio after a successful Discord bind", async () => {
    seedBrowserPlaying();
    const pause = vi.spyOn(engine, "pauseAll");
    const play = vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/discord/join")) {
        return { ok: true, guild_id: "g1", binding_revision: 4, session_id: "sess-1" };
      }
      if (String(url).includes("queue/control")) {
        return queue({
          output_pref: "discord",
          renderer_kind: "none",
          binding_revision: 4,
          playback_instance_id: "inst-1"
        });
      }
      throw new Error(`unexpected POST ${url}`);
    });
    vi.mocked(api.get).mockResolvedValue(
      queue({ output_pref: "discord", renderer_kind: "discord", renderer_id: "bot-1", binding_revision: 4 })
    );

    await usePlayer.getState().setOutput("discord");

    expect(usePlayer.getState().output).toBe("discord");
    expect(getPlayerSessionForTests().queue.playback_instance_id).toBe("inst-1");
    expect(pause).toHaveBeenCalled();
    expect(play).not.toHaveBeenCalled();
    expect(engine.htmlAudioPaused()).toBe(true);
  });

  it("starts Discord → Browser from checkpoint and retains the guild bind", async () => {
    const q = queue({
      output_pref: "discord",
      renderer_kind: "discord",
      renderer_id: "bot-1",
      position_ms: 12_500,
      binding_revision: 7
    });
    resetPlayerSessionForTests({
      ...initialSession(),
      lastAppliedRevision: 4,
      lastPlayheadSequence: 4,
      lastInstanceId: "inst-1",
      lastBindingRevision: 7,
      lastBindingByGuild: { g1: 7 },
      queue: q
    });
    usePlayer.setState({
      queue: q,
      playing: true,
      output: "discord",
      voice,
      current: { id: "t1", title: "Song" },
      position: 12_500,
      duration: 180_000
    });
    const play = vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.mocked(api.post).mockImplementation(async (url: string, body?: unknown) => {
      const path = String(url);
      if (path.includes("queue/control")) {
        const extra = (body as { extra?: { output_pref?: string } }).extra;
        expect(extra?.output_pref).toBe("browser");
        return queue({
          output_pref: "browser",
          renderer_kind: "browser",
          renderer_id: getTabRendererId(),
          position_ms: 12_500,
          binding_revision: 7,
          playback_instance_id: "inst-1"
        });
      }
      if (path.includes("renderer/acquire")) {
        return queue({
          output_pref: "browser",
          renderer_kind: "browser",
          renderer_id: getTabRendererId(),
          position_ms: 12_500,
          binding_revision: 7
        });
      }
      throw new Error(`unexpected POST ${path}`);
    });

    await usePlayer.getState().setOutput("browser");

    expect(usePlayer.getState().output).toBe("browser");
    expect(getPlayerSessionForTests().lastBindingRevision).toBe(7);
    expect(getPlayerSessionForTests().lastBindingByGuild.g1).toBe(7);
    expect(play).toHaveBeenCalled();
    expect(usePlayer.getState().position).toBeGreaterThan(12_000);
  });

  it("ignores a stale bind HTTP behind a newer binding_revision", async () => {
    seedBrowserPlaying();
    vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    resetPlayerSessionForTests({
      ...getPlayerSessionForTests(),
      lastBindingRevision: 9,
      lastBindingByGuild: { g1: 9 }
    });
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/discord/join")) {
        return { ok: true, guild_id: "g1", binding_revision: 4 };
      }
      if (String(url).includes("renderer/acquire")) {
        return queue({ renderer_id: getTabRendererId() });
      }
      return queue();
    });

    await usePlayer.getState().setOutput("discord");

    expect(usePlayer.getState().output).toBe("browser");
    expect(getPlayerSessionForTests().lastBindingRevision).toBe(9);
  });

  it("toggles back to browser and plays locally when no voice channel is visible", async () => {
    resetPlayerSessionForTests();
    usePlayer.setState({
      queue: null,
      playing: false,
      output: "discord",
      voice: { ...voice, in_voice: false },
      current: undefined,
      position: 0,
      duration: 0
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo) => {
        const url = String(input);
        if (url.includes("voice-state")) {
          return { ok: true, json: async () => ({ ...voice, in_voice: false, guild_id: null, channel_id: null }) } as Response;
        }
        return { ok: false, json: async () => ({}) } as Response;
      })
    );
    const play = vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/discord/join")) throw new Error("should not join");
      if (String(url).includes("renderer/acquire")) {
        return queue({ renderer_id: getTabRendererId(), renderer_kind: "browser", output_pref: "browser" });
      }
      throw new Error(`unexpected POST ${url}`);
    });
    vi.mocked(api.put).mockResolvedValue(
      queue({
        output_pref: "browser",
        renderer_kind: "browser",
        renderer_id: getTabRendererId(),
        status: "playing",
        current_track_id: "t1",
        position_ms: 0
      })
    );
    vi.mocked(api.get).mockResolvedValue({ id: "t1", title: "Song", duration_ms: 180_000 });

    await usePlayer.getState().playTracks(["t1"]);

    expect(api.post).not.toHaveBeenCalledWith("/api/v1/me/discord/join", expect.any(Object));
    expect(usePlayer.getState().output).toBe("browser");
    expect(play).toHaveBeenCalled();
  });

  it("auto-joins Discord and plays there when the listener is in voice", async () => {
    resetPlayerSessionForTests();
    usePlayer.setState({
      queue: null,
      playing: false,
      output: "browser",
      voice,
      current: undefined,
      position: 0,
      duration: 0
    });
    const pause = vi.spyOn(engine, "pauseAll");
    const play = vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.mocked(api.post).mockImplementation(async (url: string) => {
      if (String(url).includes("/discord/join")) {
        return { ok: true, guild_id: "g1", binding_revision: 5, session_id: "sess-1" };
      }
      throw new Error(`unexpected POST ${url}`);
    });
    vi.mocked(api.put).mockResolvedValue(
      queue({
        output_pref: "discord",
        renderer_kind: "discord",
        renderer_id: "bot-1",
        status: "playing",
        current_track_id: "t1",
        position_ms: 0,
        binding_revision: 5
      })
    );
    vi.mocked(api.get).mockResolvedValue({ id: "t1", title: "Song", duration_ms: 180_000 });

    await usePlayer.getState().playTracks(["t1"]);

    expect(api.post).toHaveBeenCalledWith("/api/v1/me/discord/join", expect.any(Object));
    expect(api.put).toHaveBeenCalled();
    expect(usePlayer.getState().output).toBe("discord");
    expect(usePlayer.getState().current?.id).toBe("t1");
    expect(pause).toHaveBeenCalled();
    expect(play).not.toHaveBeenCalled();
  });

  it("treats a live Discord session as Discord even if the device pref is browser", async () => {
    localStorage.setItem(
      "sd-device",
      JSON.stringify({ deviceId: "dev-1", outputManual: "browser", sinkId: "", autoplay: false, visualizer: false, playbackRate: 1 })
    );
    const q = queue({
      output_pref: "discord",
      renderer_kind: "discord",
      renderer_id: "bot-1",
      binding_revision: 4
    });
    resetPlayerSessionForTests({
      ...initialSession(),
      lastAppliedRevision: 4,
      lastPlayheadSequence: 4,
      lastInstanceId: "inst-1",
      lastBindingRevision: 4,
      lastBindingByGuild: { g1: 4 },
      queue: q
    });
    usePlayer.setState({
      queue: q,
      playing: true,
      output: "browser",
      voice,
      current: { id: "t1", title: "Song" },
      position: 8_000,
      duration: 180_000
    });
    const play = vi.spyOn(engine, "playActive").mockResolvedValue(undefined);
    vi.mocked(api.get).mockResolvedValue(q);

    await usePlayer.getState().syncDiscordQueue();

    expect(play).not.toHaveBeenCalled();
    expect(engine.htmlAudioPaused()).toBe(true);
  });

  it("stops HTMLAudio immediately when GET renderer_id is not this tab", async () => {
    seedBrowserPlaying();
    const pause = vi.spyOn(engine, "pauseAll");
    vi.mocked(api.get).mockResolvedValue(
      queue({ renderer_id: "other-tab", renderer_kind: "browser", output_pref: "browser" })
    );

    await usePlayer.getState().syncDiscordQueue();

    expect(pause).toHaveBeenCalled();
    expect(getPlayerSessionForTests().stopAudio).toBe(true);
    expect(engine.htmlAudioPaused()).toBe(true);
  });
});
