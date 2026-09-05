import { describe, expect, it } from "vitest";
import { activeLyricIndex, activeWordIndex, karaokeLines } from "./lyricsSync";

const lines = [
  { t_ms: 0, text: "intro" },
  { t_ms: 1_000, text: "verse" },
  { t_ms: 4_000, text: "chorus" },
  { t_ms: 8_000, text: "outro" }
];

describe("activeLyricIndex", () => {
  it("returns -1 when there are no lines", () => {
    expect(activeLyricIndex(undefined, 0)).toBe(-1);
    expect(activeLyricIndex([], 500)).toBe(-1);
  });

  it("picks the last line whose t_ms is at or before the playhead", () => {
    expect(activeLyricIndex(lines, 0)).toBe(0);
    expect(activeLyricIndex(lines, 999)).toBe(0);
    expect(activeLyricIndex(lines, 1_000)).toBe(1);
    expect(activeLyricIndex(lines, 3_999)).toBe(1);
    expect(activeLyricIndex(lines, 4_000)).toBe(2);
    expect(activeLyricIndex(lines, 12_000)).toBe(3);
  });

  it("stays on the latest matching timestamp when lines are unsorted", () => {
    const shuffled = [
      { t_ms: 4_000, text: "chorus" },
      { t_ms: 0, text: "intro" },
      { t_ms: 1_000, text: "verse" }
    ];
    expect(activeLyricIndex(shuffled, 2_500)).toBe(2);
    expect(activeLyricIndex(shuffled, 4_000)).toBe(0);
  });
});

describe("activeWordIndex", () => {
  const words = [
    { t_ms: 1000, text: "Hello" },
    { t_ms: 1400, text: "beautiful" },
    { t_ms: 2200, text: "world" }
  ];

  it("highlights the word that is currently hitting", () => {
    expect(activeWordIndex(words, 999)).toBe(-1);
    expect(activeWordIndex(words, 1000)).toBe(0);
    expect(activeWordIndex(words, 1399)).toBe(0);
    expect(activeWordIndex(words, 1400)).toBe(1);
    expect(activeWordIndex(words, 3000)).toBe(2);
  });
});

describe("karaokeLines", () => {
  it("interpolates words when the API only sent line times", () => {
    const got = karaokeLines({
      body: "[00:00.00] Hello world\n[00:04.00] next",
      timed: true,
      source: "lrclib",
      lines: [
        { t_ms: 0, text: "Hello world" },
        { t_ms: 4000, text: "next" }
      ]
    });
    expect(got[0].words?.map((w) => w.text)).toEqual(["Hello", "world"]);
    expect(got[0].words?.[0].t_ms).toBe(0);
    expect(got[0].words?.[1].t_ms).toBeGreaterThan(0);
    expect(got[0].words?.[1].t_ms).toBeLessThan(4000);
  });

  it("keeps provider word timestamps", () => {
    const got = karaokeLines({
      body: "",
      timed: true,
      lines: [{ t_ms: 0, text: "Hello world", words: [{ t_ms: 50, text: "Hello" }, { t_ms: 400, text: "world" }] }]
    });
    expect(got[0].words).toEqual([
      { t_ms: 50, text: "Hello" },
      { t_ms: 400, text: "world" }
    ]);
  });
});
