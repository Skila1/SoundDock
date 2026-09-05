import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Mic2, X } from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl } from "@/lib/utils";
import { api } from "@/lib/api";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import type { LyricsLine, LyricsWord, TrackLyrics } from "@/types/api";
import { activeLyricIndex, activeWordIndex, hasPlainBody, karaokeLines, lyricsQueryKey } from "./lyricsSync";

function useSmoothPosition(storePos: number, playing: boolean, rate: number) {
  const [pos, setPos] = useState(storePos);
  const base = useRef({ storePos, at: performance.now(), playing, rate });

  useEffect(() => {
    base.current = { storePos, at: performance.now(), playing, rate };
    setPos(storePos);
  }, [storePos, playing, rate]);

  useEffect(() => {
    if (!playing) return;
    let raf = 0;
    const tick = () => {
      const b = base.current;
      const next = b.storePos + Math.max(0.25, b.rate || 1) * (performance.now() - b.at);
      setPos(next);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [playing]);

  return playing ? pos : storePos;
}

function WordButton({
  word,
  state,
  onSeek
}: {
  word: LyricsWord;
  state: "past" | "active" | "future";
  onSeek: (ms: number) => void;
}) {
  const cls =
    state === "active"
      ? "text-accent"
      : state === "past"
        ? "text-foreground"
        : "text-muted/55";
  return (
    <button
      type="button"
      className={`rounded-sm px-0.5 transition-colors duration-150 hover:text-accent ${cls}`}
      onClick={(e) => {
        e.stopPropagation();
        onSeek(word.t_ms);
      }}
    >
      {word.text}
    </button>
  );
}

function KaraokeLine({
  line,
  active,
  compact,
  positionMs,
  onSeek
}: {
  line: LyricsLine;
  active: boolean;
  compact?: boolean;
  positionMs: number;
  onSeek: (ms: number) => void;
}) {
  const words = line.words || [];
  const wordIdx = active ? activeWordIndex(words, positionMs) : -1;
  return (
    <li data-active={active ? "true" : undefined}>
      <button
        type="button"
        onClick={() => onSeek(line.t_ms)}
        className={`block w-full text-left leading-relaxed transition-all duration-200 ${
          compact
            ? active
              ? "text-base font-semibold text-foreground"
              : "text-sm text-muted"
            : active
              ? "text-2xl font-semibold tracking-tight text-foreground md:text-3xl"
              : "text-lg text-muted/70 md:text-xl"
        }`}
      >
        {words.length ? (
          <span className={`flex flex-wrap gap-x-1.5 gap-y-1 ${compact ? "" : "justify-center"}`}>
            {words.map((word, i) => (
              <WordButton
                key={`${word.t_ms}-${i}`}
                word={word}
                state={!active ? "future" : i === wordIdx ? "active" : i < wordIdx ? "past" : "future"}
                onSeek={onSeek}
              />
            ))}
          </span>
        ) : (
          line.text || "\u00a0"
        )}
      </button>
    </li>
  );
}

export function LyricsKaraoke({
  lyrics,
  positionMs,
  compact,
  onSeek
}: {
  lyrics: TrackLyrics;
  positionMs: number;
  compact?: boolean;
  onSeek: (ms: number) => void;
}) {
  const listRef = useRef<HTMLDivElement>(null);
  const lines = karaokeLines(lyrics);
  const activeIdx = activeLyricIndex(lines, positionMs);

  useEffect(() => {
    if (activeIdx < 0) return;
    const el = listRef.current?.querySelector("[data-active='true']");
    el?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [activeIdx]);

  if (lines.length) {
    return (
      <div
        ref={listRef}
        className={compact ? "max-h-52 overflow-y-auto rounded-lg bg-surface-2/70 px-4 py-3" : "min-h-0 flex-1 overflow-y-auto px-4 py-6"}
        role="region"
        aria-label="Lyrics"
      >
        <ul className={compact ? "space-y-2" : "mx-auto flex max-w-2xl flex-col gap-5 py-[30vh]"}>
          {lines.map((line, i) => (
            <KaraokeLine
              key={`${line.t_ms}-${i}`}
              line={line}
              active={i === activeIdx}
              compact={compact}
              positionMs={positionMs}
              onSeek={onSeek}
            />
          ))}
        </ul>
      </div>
    );
  }

  if (hasPlainBody(lyrics)) {
    return (
      <div
        className={compact ? "max-h-52 overflow-y-auto rounded-lg bg-surface-2/70 px-4 py-3" : "min-h-0 flex-1 overflow-y-auto px-6 py-8"}
        role="region"
        aria-label="Lyrics"
      >
        <p className={`mx-auto max-w-xl whitespace-pre-wrap leading-relaxed text-muted ${compact ? "text-sm" : "text-lg"}`}>
          {lyrics.body}
        </p>
      </div>
    );
  }

  return null;
}

export function LyricsPrefetch() {
  const currentId = usePlayer((s) => s.current?.id);
  const items = usePlayer((s) => s.queue?.items);
  const idx = usePlayer((s) => s.queue?.current_index ?? -1);
  const nextId = items && idx >= 0 ? items[idx + 1]?.track_id : undefined;

  useQuery({
    queryKey: lyricsQueryKey(currentId || ""),
    queryFn: () => api.get<TrackLyrics>(`/api/v1/tracks/${encodeURIComponent(currentId!)}/lyrics`),
    enabled: Boolean(currentId),
    staleTime: 10 * 60_000
  });
  useQuery({
    queryKey: lyricsQueryKey(nextId || ""),
    queryFn: () => api.get<TrackLyrics>(`/api/v1/tracks/${encodeURIComponent(nextId!)}/lyrics`),
    enabled: Boolean(nextId),
    staleTime: 10 * 60_000
  });
  return null;
}

export function LyricsView() {
  const ui = useUi();
  const p = usePlayer();
  const t = p.current;
  const positionMs = useSmoothPosition(p.position, p.playing && ui.lyricsOpen, p.playbackRate || 1);
  const q = useQuery({
    queryKey: lyricsQueryKey(t?.id || ""),
    queryFn: () => api.get<TrackLyrics>(`/api/v1/tracks/${encodeURIComponent(t!.id)}/lyrics`),
    enabled: ui.lyricsOpen && Boolean(t?.id),
    staleTime: 10 * 60_000
  });
  const lyrics = q.data;
  const showKaraoke = Boolean(lyrics && (karaokeLines(lyrics).length || hasPlainBody(lyrics)));

  return (
    <Dialog open={ui.lyricsOpen} onOpenChange={(open) => ui.set({ lyricsOpen: open })}>
      <DialogContent
        hideClose
        overlayClassName="bottom-[72px]"
        className="fixed inset-x-0 top-0 bottom-[72px] left-0 z-50 flex h-auto max-h-none w-full max-w-none translate-x-0 translate-y-0 flex-col overflow-hidden rounded-none border-0 bg-background p-0 sm:w-full"
      >
        <div className="flex items-center gap-3 border-b border-border px-4 py-3">
          <div className="h-12 w-12 shrink-0 overflow-hidden rounded-md bg-surface-2">
            {t && <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" />}
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{t?.title || "Nothing playing"}</div>
            <div className="truncate text-xs text-muted">{t?.artists?.map((a) => a.name).join(", ") || t?.artist || ""}</div>
          </div>
          <Button size="icon" variant="ghost" onClick={() => ui.set({ lyricsOpen: false })} aria-label="Close lyrics">
            <X />
          </Button>
        </div>
        {q.isLoading && t ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted">Loading lyrics…</div>
        ) : showKaraoke && lyrics ? (
          <LyricsKaraoke lyrics={lyrics} positionMs={positionMs} onSeek={(ms) => p.seek(ms)} />
        ) : (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
            <div className="rounded-full bg-surface-2 p-3">
              <Mic2 className="h-6 w-6 text-muted" />
            </div>
            <p className="text-base font-semibold">{t ? "No lyrics for this track" : "Nothing playing"}</p>
            <p className="max-w-sm text-sm text-muted">
              {t
                ? "SoundDock looks up cached, embedded, and on-disk lyrics, then LRCLIB when that is enabled in Admin."
                : "Play a song, then open lyrics again."}
            </p>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
