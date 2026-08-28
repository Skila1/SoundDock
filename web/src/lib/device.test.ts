import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import {
  RENDERER_CHANNEL,
  askRendererTabsToStop,
  getTabRendererGeneration,
  getTabRendererId,
  subscribeRendererChannel
} from "@/lib/device";

describe("tab renderer identity", () => {
  beforeEach(() => {
    sessionStorage.clear();
    localStorage.clear();
  });

  it("stores renderer_id in sessionStorage, not localStorage", () => {
    const id = getTabRendererId();
    expect(id).toMatch(/^[0-9a-f-]{36}$/i);
    expect(sessionStorage.getItem("sd-renderer-id")).toBe(id);
    expect(localStorage.getItem("sd-renderer-id")).toBeNull();
    expect(getTabRendererId()).toBe(id);
    expect(getTabRendererGeneration()).toBe(1);
  });
});

class FakeBroadcastChannel {
  static channels = new Map<string, FakeBroadcastChannel[]>();
  name: string;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  constructor(name: string) {
    this.name = name;
    const list = FakeBroadcastChannel.channels.get(name) ?? [];
    list.push(this);
    FakeBroadcastChannel.channels.set(name, list);
  }
  postMessage(data: unknown) {
    for (const ch of FakeBroadcastChannel.channels.get(this.name) ?? []) {
      if (ch === this) continue;
      ch.onmessage?.({ data } as MessageEvent);
    }
  }
  close() {
    const list = (FakeBroadcastChannel.channels.get(this.name) ?? []).filter((c) => c !== this);
    FakeBroadcastChannel.channels.set(this.name, list);
  }
}

describe("two-tab renderer BroadcastChannel", () => {
  beforeEach(() => {
    sessionStorage.clear();
    localStorage.clear();
    FakeBroadcastChannel.channels.clear();
    vi.stubGlobal("BroadcastChannel", FakeBroadcastChannel);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("asks other tabs to stop HTMLAudio without stopping the sender", () => {
    const stop = vi.fn();
    sessionStorage.setItem("sd-renderer-id", "tab-a");
    subscribeRendererChannel(stop);
    const other = new FakeBroadcastChannel(RENDERER_CHANNEL);
    other.postMessage({ type: "stop-request", renderer_id: "tab-b" });
    expect(stop).toHaveBeenCalledOnce();
    other.postMessage({ type: "stop-request", renderer_id: "tab-a" });
    expect(stop).toHaveBeenCalledOnce();
    expect(RENDERER_CHANNEL).toBe("sd-renderer");
    askRendererTabsToStop();
    expect(stop).toHaveBeenCalledOnce();
  });
});
