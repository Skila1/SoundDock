import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { desktopQueue, queueDocked, useUi } from "./ui";

function stubDesktop(on: boolean) {
  window.matchMedia = ((query: string) => ({
    matches: on && query.includes("1280"),
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() {
      return false;
    }
  })) as typeof window.matchMedia;
}

const reset = {
  lyricsOpen: false,
  nowPlayingOpen: false,
  queueOpen: false,
  queueCollapsed: true,
  queuePinned: false
};

describe("lyrics side panel vs queue", () => {
  beforeEach(() => {
    stubDesktop(true);
    useUi.setState(reset);
  });

  afterEach(() => {
    useUi.setState(reset);
  });

  it("detects a docked desktop queue", () => {
    expect(desktopQueue()).toBe(true);
    expect(queueDocked({ queuePinned: false, queueCollapsed: true })).toBe(false);
    expect(queueDocked({ queuePinned: true, queueCollapsed: true })).toBe(true);
    expect(queueDocked({ queuePinned: false, queueCollapsed: false })).toBe(true);
  });

  it("opens lyrics without clearing a docked queue so it can come back", () => {
    useUi.setState({ queueCollapsed: false, queuePinned: true });
    useUi.getState().openLyrics();
    expect(useUi.getState().lyricsOpen).toBe(true);
    expect(useUi.getState().queueCollapsed).toBe(false);
    expect(useUi.getState().queuePinned).toBe(true);
    useUi.getState().toggleLyrics();
    expect(useUi.getState().lyricsOpen).toBe(false);
    expect(queueDocked(useUi.getState())).toBe(true);
  });

  it("closes lyrics and leaves the queue hidden when it was not open", () => {
    useUi.getState().toggleLyrics();
    expect(useUi.getState().lyricsOpen).toBe(true);
    useUi.getState().toggleLyrics();
    expect(useUi.getState().lyricsOpen).toBe(false);
    expect(queueDocked(useUi.getState())).toBe(false);
  });

  it("switches from lyrics back to the queue when the queue button is used", () => {
    useUi.setState({ queueCollapsed: true, queuePinned: false, lyricsOpen: true });
    useUi.getState().toggleQueue();
    expect(useUi.getState().lyricsOpen).toBe(false);
    expect(useUi.getState().queueCollapsed).toBe(false);
  });
});
