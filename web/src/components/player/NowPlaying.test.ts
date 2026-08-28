import { describe, expect, it } from "vitest";
import { activeLyricIndex } from "./NowPlaying";

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
