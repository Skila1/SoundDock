import type { LyricsLine, LyricsWord, TrackLyrics } from "@/types/api";

/** Last cue whose t_ms is at or before the interpolated playhead. */
export function activeLyricIndex(lines: ReadonlyArray<{ t_ms?: number }> | undefined, positionMs: number): number {
  if (!lines?.length) return -1;
  let idx = -1;
  let best = -Infinity;
  for (let i = 0; i < lines.length; i++) {
    const t = lines[i]?.t_ms;
    if (typeof t === "number" && Number.isFinite(t) && t <= positionMs && t >= best) {
      best = t;
      idx = i;
    }
  }
  return idx;
}

export function activeWordIndex(words: ReadonlyArray<{ t_ms?: number }> | undefined, positionMs: number): number {
  return activeLyricIndex(words, positionMs);
}

export function hasTimedLines(l: TrackLyrics | null | undefined): l is TrackLyrics & { lines: LyricsLine[] } {
  return Boolean(l?.timed && l.lines?.some((line) => typeof line.t_ms === "number"));
}

export function hasPlainBody(l: TrackLyrics | null | undefined): boolean {
  return typeof l?.body === "string" && l.body.trim().length > 0;
}

export function lyricsQueryKey(trackId: string) {
  return ["track-lyrics", trackId] as const;
}

function splitWords(text: string): string[] {
  return text.split(/\s+/).filter(Boolean);
}

function interpolateWords(text: string, start: number, end: number): LyricsWord[] {
  const parts = splitWords(text);
  if (!parts.length) return [];
  const span = end > start ? end - start : Math.max(1000, parts.length * 400);
  const weights = parts.map((p) => Math.max(1, [...p].length));
  const total = weights.reduce((a, b) => a + b, 0);
  let acc = 0;
  return parts.map((text, i) => {
    const word = { t_ms: start + Math.round((span * acc) / total), text };
    acc += weights[i];
    return word;
  });
}

/** Timed lines with word cues (from API or interpolated on the client). */
export function karaokeLines(lyrics: TrackLyrics | null | undefined): LyricsLine[] {
  if (!hasTimedLines(lyrics)) return [];
  return lyrics.lines.map((line, i) => {
    if (line.words?.length) return line;
    const next = lyrics.lines[i + 1];
    const end = typeof next?.t_ms === "number" && next.t_ms > line.t_ms ? next.t_ms : line.t_ms + 4000;
    return { ...line, words: interpolateWords(line.text || "", line.t_ms, end) };
  });
}
