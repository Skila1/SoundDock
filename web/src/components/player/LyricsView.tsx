import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Mic2, PanelRightClose, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl } from "@/lib/utils";
import { api } from "@/lib/api";
import { desktopQueue, useUi } from "@/stores/ui";
import { usePlayer } from "@/stores/player";
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

function useDockedSide() {
  const [docked, setDocked] = useState(() => desktopQueue());
  useEffect(() => {
    const mq = window.matchMedia("(min-width: 1280px)");
    const sync = () => setDocked(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);
  return docked;
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
              ? "text-sm font-semibold text-foreground"
              : "text-sm text-muted"
            : active
              ? "text-base font-semibold text-foreground"
              : "text-sm text-muted"
        }`}
      >
        {words.length ? (
          <span className="flex flex-wrap gap-x-1.5 gap-y-1">
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
        className={compact ? "max-h-52 overflow-y-auto rounded-lg bg-surface-2/70 px-4 py-3" : "min-h-0 flex-1 overflow-y-auto px-4 py-2"}
        role="region"
        aria-label="Lyrics"
      >
        <ul className={compact ? "space-y-2" : "space-y-3 py-2"}>
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
        className={compact ? "max-h-52 overflow-y-auto rounded-lg bg-surface-2/70 px-4 py-3" : "min-h-0 flex-1 overflow-y-auto px-4 py-3"}
        role="region"
        aria-label="Lyrics"
      >
        <p className="whitespace-pre-wrap text-sm leading-relaxed text-muted">{lyrics.body}</p>
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

export function LyricsPanel({ onCollapse, onClose }: { onCollapse?: () => void; onClose?: () => void }) {
  const p = usePlayer();
  const t = p.current;
  const positionMs = useSmoothPosition(p.position, p.playing, p.playbackRate || 1);
  const q = useQuery({
    queryKey: lyricsQueryKey(t?.id || ""),
    queryFn: () => api.get<TrackLyrics>(`/api/v1/tracks/${encodeURIComponent(t!.id)}/lyrics`),
    enabled: Boolean(t?.id),
    staleTime: 10 * 60_000
  });
  const lyrics = q.data;
  const showKaraoke = Boolean(lyrics && (karaokeLines(lyrics).length || hasPlainBody(lyrics)));

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between gap-2 px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <div className="h-9 w-9 shrink-0 overflow-hidden rounded-md bg-surface-2">
            {t && <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" />}
          </div>
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold">{t?.title || "Lyrics"}</h2>
            <p className="truncate text-xs text-muted">{t?.artists?.map((a) => a.name).join(", ") || t?.artist || ""}</p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {onCollapse && (
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onCollapse} aria-label="Close lyrics">
              <PanelRightClose className="h-4 w-4" />
            </Button>
          )}
          {onClose && (
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onClose} aria-label="Close lyrics">
              <X className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
      {q.isLoading && t ? (
        <div className="flex flex-1 items-center justify-center px-4 text-sm text-muted">Loading lyrics…</div>
      ) : showKaraoke && lyrics ? (
        <LyricsKaraoke lyrics={lyrics} positionMs={positionMs} onSeek={(ms) => p.seek(ms)} />
      ) : (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <div className="rounded-full bg-surface-2 p-3">
            <Mic2 className="h-5 w-5 text-muted" />
          </div>
          <p className="text-sm font-semibold">{t ? "No lyrics for this track" : "Nothing playing"}</p>
          <p className="text-xs text-muted">
            {t
              ? "Cached, embedded, and on-disk lyrics are used first. LRCLIB is used when enabled in Admin."
              : "Play a song, then open lyrics again."}
          </p>
        </div>
      )}
    </div>
  );
}

export function LyricsSheet() {
  const ui = useUi();
  const docked = useDockedSide();
  return (
    <Sheet open={ui.lyricsOpen && !docked} onOpenChange={(open) => !open && ui.closeLyrics()}>
      <SheetContent
        side={typeof window !== "undefined" && window.innerWidth < 768 ? "bottom" : "right"}
        className="flex flex-col p-0"
        hideClose
      >
        <LyricsPanel onClose={() => ui.closeLyrics()} />
      </SheetContent>
    </Sheet>
  );
}
