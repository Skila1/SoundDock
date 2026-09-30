import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api", () => ({ api: { post: vi.fn().mockResolvedValue({ ok: true }), get: vi.fn() } }));

import { createQueueSseClient, STREAM_STALE_MS } from "@/stores/sseClient";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  readyState = FakeEventSource.CONNECTING;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((ev: MessageEvent) => void)[]>();
  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }
  addEventListener(name: string, fn: (ev: MessageEvent) => void) {
    this.listeners.set(name, [...(this.listeners.get(name) || []), fn]);
  }
  emit(name: string, data: unknown) {
    for (const fn of this.listeners.get(name) || []) fn({ data: JSON.stringify(data) } as MessageEvent);
  }
  open() {
    this.readyState = FakeEventSource.OPEN;
    this.onopen?.();
  }
  close() {
    this.readyState = FakeEventSource.CLOSED;
  }
}

describe("queue SSE watchdog", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("reconnects and resyncs a stream that went silent without an error", async () => {
    const fetchSnapshot = vi.fn().mockResolvedValue(null);
    const client = createQueueSseClient({
      fetchSnapshot,
      onSnapshot: () => undefined,
      onState: () => undefined,
      onPlayhead: () => undefined,
      onPresence: () => undefined
    });
    client.start({ resync: false });
    expect(FakeEventSource.instances).toHaveLength(1);
    FakeEventSource.instances[0].open();

    await vi.advanceTimersByTimeAsync(STREAM_STALE_MS + 6_000);

    expect(fetchSnapshot).toHaveBeenCalled();
    expect(FakeEventSource.instances.length).toBeGreaterThan(1);
    expect(FakeEventSource.instances[0].readyState).toBe(FakeEventSource.CLOSED);
    client.stop();
  });

  it("leaves a stream alone while pings keep arriving", async () => {
    const fetchSnapshot = vi.fn().mockResolvedValue(null);
    const client = createQueueSseClient({
      fetchSnapshot,
      onSnapshot: () => undefined,
      onState: () => undefined,
      onPlayhead: () => undefined,
      onPresence: () => undefined
    });
    client.start({ resync: false });
    const es = FakeEventSource.instances[0];
    es.open();
    for (let i = 0; i < 6; i++) {
      await vi.advanceTimersByTimeAsync(15_000);
      es.emit("ping", {});
    }
    expect(fetchSnapshot).not.toHaveBeenCalled();
    expect(FakeEventSource.instances).toHaveLength(1);
    client.stop();
  });
});
