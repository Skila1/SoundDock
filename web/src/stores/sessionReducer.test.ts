import { describe, expect, it } from "vitest";
import {
  applyBindResult,
  applyJoinFailure,
  applySnapshot,
  applySwitchToBrowser,
  initialSession,
  shouldStopHtmlAudio,
  tabOwnsBrowserLease,
  type QueueSnapshot
} from "@/stores/sessionReducer";

function snap(partial: Partial<QueueSnapshot> = {}): QueueSnapshot {
  return {
    id: "sess-1",
    status: "paused",
    volume: 0.8,
    shuffle: false,
    repeat: "off",
    crossfade_seconds: 0,
    replaygain_mode: "off",
    current_index: 0,
    current_track_id: "t1",
    position_ms: 1_000,
    items: [{ id: "i1", position: 0, track_id: "t1" }],
    state_revision: 1,
    playhead_sequence: 1,
    playback_instance_id: "inst-1",
    muted: false,
    output_pref: "browser",
    renderer_kind: "browser",
    renderer_id: "tab-a",
    checkpoint_at: new Date(1_000).toISOString(),
    duration_ms: 60_000,
    playback_rate: 1,
    binding_revision: 1,
    ...partial
  };
}

describe("apply snapshot", () => {
  it("applies queue identity, volume, and playhead from a session snapshot", () => {
    const next = applySnapshot(initialSession(), snap({ status: "playing", volume: 0.4, position_ms: 2500 }), {
      nowMs: 1_000,
      clock: { localSend: 1_000, localReceive: 1_000, serverTime: 1_000, offsetMs: 0 }
    });
    expect(next.ignored).toBeNull();
    expect(next.stateApplied).toBe(true);
    expect(next.queue.current_track_id).toBe("t1");
    expect(next.queue.volume).toBe(0.4);
    expect(next.queue.muted).toBe(false);
    expect(next.lastAppliedRevision).toBe(1);
    expect(next.playhead.checkpointPositionMs).toBe(2500);
    expect(next.playhead.playing).toBe(true);
  });
});

describe("stale state_revision", () => {
  it("ignores a snapshot whose state_revision is older than last applied", () => {
    const first = applySnapshot(initialSession(), snap({ state_revision: 5, volume: 0.5 }));
    const stale = applySnapshot(first, snap({ state_revision: 4, volume: 0.1, status: "playing" }));
    expect(stale.ignored).toBe("stale_revision");
    expect(stale.stateApplied).toBe(false);
    expect(stale.queue.volume).toBe(0.5);
    expect(stale.queue.status).toBe("paused");
  });
});

describe("stale playhead_sequence", () => {
  it("ignores an older sequence on the same instance", () => {
    const first = applySnapshot(initialSession(), snap({ playhead_sequence: 4, position_ms: 8_000 }));
    const stale = applySnapshot(first, snap({ playhead_sequence: 3, position_ms: 1_000, state_revision: 1 }));
    expect(stale.ignored).toBe("stale_playhead");
    expect(stale.playheadApplied).toBe(false);
    expect(stale.playhead.checkpointPositionMs).toBe(8_000);
  });

  it("accepts a backward seek when playhead_sequence increased", () => {
    const first = applySnapshot(initialSession(), snap({ playhead_sequence: 2, position_ms: 9_000 }));
    const seek = applySnapshot(first, snap({ playhead_sequence: 3, position_ms: 500, state_revision: 2 }));
    expect(seek.ignored).toBeNull();
    expect(seek.playheadApplied).toBe(true);
    expect(seek.playhead.checkpointPositionMs).toBe(500);
  });
});

describe("bind revision", () => {
  it("drops a stale bind behind a newer binding_revision for the guild", () => {
    const seeded = applyBindResult(initialSession(), { binding_revision: 9, guild_id: "g1" }, "g1");
    const stale = applyBindResult(seeded, { binding_revision: 4, guild_id: "g1", ok: true }, "g1");
    expect(stale.ignored).toBe("stale_bind");
    expect(stale.lastBindingRevision).toBe(9);
    expect(stale.queue.output_pref).toBe("discord");
  });

  it("keeps guild bind when switching Discord → Browser", () => {
    const bound = applyBindResult(initialSession(), { binding_revision: 6, guild_id: "g1" }, "g1");
    const browser = applySwitchToBrowser(
      bound,
      snap({ output_pref: "browser", renderer_kind: "browser", renderer_id: "tab-a", binding_revision: 6, position_ms: 12_000 })
    );
    expect(browser.lastBindingRevision).toBe(6);
    expect(browser.queue.output_pref).toBe("browser");
    expect(browser.playhead.checkpointPositionMs).toBe(12_000);
  });
});

describe("output switch helpers", () => {
  it("keeps browser output after a failed join", () => {
    const view = applySnapshot(initialSession(), snap({ output_pref: "browser", renderer_id: "tab-a" }));
    const failed = applyJoinFailure({ ...view, queue: { ...view.queue, output_pref: "discord" } });
    expect(failed.queue.output_pref).toBe("browser");
    expect(failed.stopAudio).toBe(false);
  });

  it("stops HTMLAudio when renderer_id is not this tab", () => {
    const q = snap({ renderer_id: "other-tab", renderer_kind: "browser", output_pref: "browser" });
    expect(shouldStopHtmlAudio(q, "tab-a")).toBe(true);
    expect(tabOwnsBrowserLease(q, "tab-a")).toBe(false);
    const applied = applySnapshot(initialSession(), q, { tabRendererId: "tab-a" });
    expect(applied.stopAudio).toBe(true);
  });

  it("stops HTMLAudio when output_pref is discord", () => {
    expect(shouldStopHtmlAudio(snap({ output_pref: "discord", renderer_kind: "discord" }), "tab-a")).toBe(true);
  });

  it("unlinked user GET does not keep a guild queue", () => {
    const guild = applySnapshot(initialSession(), snap({ id: "guild-sess", items: [{ id: "i1", position: 0, track_id: "t1" }] }));
    const personal = applySnapshot(guild, snap({
      id: "personal-web",
      items: [],
      current_track_id: null,
      state_revision: 2
    }));
    expect(personal.queue.id).toBe("personal-web");
    expect(personal.queue.items).toEqual([]);
  });

  it("leave-VC guest falls back to a personal session without copying queue items", () => {
    const shared = applySnapshot(initialSession(), snap({
      id: "shared",
      items: [
        { id: "i1", position: 0, track_id: "t1" },
        { id: "i2", position: 1, track_id: "t2" }
      ]
    }));
    const personal = applySnapshot(shared, snap({
      id: "web-device",
      items: [],
      current_track_id: null,
      state_revision: 8
    }));
    expect(personal.queue.id).toBe("web-device");
    expect(personal.queue.items).toHaveLength(0);
  });

  it("owner leaving VC keeps the same session and queue ids", () => {
    const owned = applySnapshot(initialSession(), snap({ id: "web-1", binding_revision: 4 }));
    const after = applySnapshot(owned, snap({ id: "web-1", binding_revision: 5, state_revision: 2 }));
    expect(after.queue.id).toBe("web-1");
    expect(after.queue.items.map((i) => i.id)).toEqual(["i1"]);
  });

  it("GET after SSE drop during output switch reconstructs pref, lease, instance, and bind", () => {
    const midSwitch = applySnapshot(initialSession(), snap({
      output_pref: "discord",
      renderer_kind: "discord",
      renderer_id: "bot-1",
      playback_instance_id: "inst-1",
      binding_revision: 7,
      state_revision: 4
    }));
    const reconstructed = applySnapshot(midSwitch, snap({
      output_pref: "browser",
      renderer_kind: "browser",
      renderer_id: "tab-a",
      playback_instance_id: "inst-1",
      binding_revision: 7,
      state_revision: 5
    }), { tabRendererId: "tab-a" });
    expect(reconstructed.queue.output_pref).toBe("browser");
    expect(reconstructed.queue.renderer_id).toBe("tab-a");
    expect(reconstructed.queue.playback_instance_id).toBe("inst-1");
    expect(reconstructed.lastBindingRevision).toBe(7);
    expect(reconstructed.stopAudio).toBe(false);
  });

  it("stale tab after another browser took the lease stops HTMLAudio", () => {
    const view = applySnapshot(initialSession(), snap({
      renderer_id: "other-tab",
      renderer_kind: "browser",
      output_pref: "browser"
    }), { tabRendererId: "tab-a" });
    expect(view.stopAudio).toBe(true);
    expect(shouldStopHtmlAudio(view.queue, "tab-a")).toBe(true);
  });
});
