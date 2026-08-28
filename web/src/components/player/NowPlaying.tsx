import { useEffect, useRef, useState } from "react";
import { Heart, ListMusic, Pause, Play, Repeat, Repeat1, Shuffle, SkipBack, SkipForward, Square, X } from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl, formatDuration } from "@/lib/utils";
import { Visualizer } from "@/components/player/Visualizer";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import { toast } from "sonner";

/** Local until W9-lyrics lands types on api.ts. */
type LyricLine = { t_ms: number; text?: string };
type TrackLyrics = { timed?: boolean; lines?: LyricLine[]; body?: string };

function nextRepeat(mode: string) {
  if (mode === "off") return "queue";
  if (mode === "queue") return "one";
  return "off";
}

/** Last line whose t_ms is at or before the interpolated playhead. */
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

function hasTimedLines(l: TrackLyrics | null): l is TrackLyrics & { lines: LyricLine[] } {
  return Boolean(l?.timed && l.lines?.some((line) => typeof line.t_ms === "number"));
}

function hasPlainBody(l: TrackLyrics | null): boolean {
  return typeof l?.body === "string" && l.body.trim().length > 0;
}

function LyricsPanel({ lyrics, positionMs }: { lyrics: TrackLyrics; positionMs: number }) {
  const listRef = useRef<HTMLDivElement>(null);
  const timed = hasTimedLines(lyrics);
  const activeIdx = timed ? activeLyricIndex(lyrics.lines, positionMs) : -1;

  useEffect(() => {
    if (activeIdx < 0) return;
    const el = listRef.current?.querySelector("[data-active='true']");
    el?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [activeIdx]);

  if (timed) {
    return (
      <div ref={listRef} className="mt-4 max-h-52 overflow-y-auto rounded-lg bg-surface-2/70 px-4 py-3" role="region" aria-label="Lyrics">
        <ul className="space-y-2">
          {lyrics.lines.map((line, i) => {
            const active = i === activeIdx;
            return (
              <li
                key={`${line.t_ms}-${i}`}
                data-active={active ? "true" : undefined}
                className={`text-sm leading-relaxed transition-colors ${active ? "font-semibold text-accent" : "text-muted"}`}
              >
                {line.text || "\u00a0"}
              </li>
            );
          })}
        </ul>
      </div>
    );
  }

  if (hasPlainBody(lyrics)) {
    return (
      <div className="mt-4 max-h-52 overflow-y-auto rounded-lg bg-surface-2/70 px-4 py-3" role="region" aria-label="Lyrics">
        <p className="whitespace-pre-wrap text-sm leading-relaxed text-muted">{lyrics.body}</p>
      </div>
    );
  }

  return null;
}

export function NowPlaying() {
  const ui = useUi();
  const p = usePlayer();
  const t = p.current;
  const progress = p.duration ? (p.position / p.duration) * 100 : 0;
  const RepeatIcon = p.repeat === "one" ? Repeat1 : Repeat;
  const [lyrics, setLyrics] = useState<TrackLyrics | null>(null);

  useEffect(() => {
    if (!ui.nowPlayingOpen || !t?.id) {
      setLyrics(null);
      return;
    }
    let cancelled = false;
    api
      .get<TrackLyrics>(`/api/v1/tracks/${encodeURIComponent(t.id)}/lyrics`)
      .then((data) => {
        if (cancelled) return;
        if (!data || typeof data !== "object") {
          setLyrics(null);
          return;
        }
        setLyrics(data);
      })
      .catch(() => {
        if (!cancelled) setLyrics(null);
      });
    return () => {
      cancelled = true;
    };
  }, [ui.nowPlayingOpen, t?.id]);

  const showLyrics = Boolean(lyrics && (hasTimedLines(lyrics) || hasPlainBody(lyrics)));

  return (
    <Dialog open={ui.nowPlayingOpen} onOpenChange={(v) => ui.set({ nowPlayingOpen: v })}>
      <DialogContent className="max-h-[92vh] overflow-auto p-0 sm:w-[min(560px,96vw)]">
        <div className="relative bg-surface-1 p-6">
          <Button size="icon" variant="ghost" className="absolute right-3 top-3" onClick={() => ui.set({ nowPlayingOpen: false })} aria-label="Close">
            <X />
          </Button>
          <div className="mx-auto aspect-square w-full max-w-sm overflow-hidden rounded-xl shadow-card">
            {t && <Artwork src={artworkUrl("track", t.id, "now")} id={t.id} name={t.title} kind="album" />}
          </div>
          <Visualizer active={p.visualizer && ui.nowPlayingOpen} />
          <div className="mt-6">
            <h2 className="text-2xl font-semibold">{t?.title || "Nothing playing"}</h2>
            <p className="text-muted">{t?.artists?.map((a) => a.name).join(", ") || t?.artist}</p>
            <p className="text-sm text-subtle">{t?.album}</p>
          </div>
          {showLyrics && lyrics && <LyricsPanel lyrics={lyrics} positionMs={p.position} />}
          <div className="mt-4 flex items-center gap-2">
            <span className="w-10 text-xs text-subtle">{formatDuration(p.position)}</span>
            <Slider value={[progress]} onValueChange={([v]) => p.seek(((v || 0) / 100) * (p.duration || 0))} />
            <span className="w-10 text-xs text-subtle">{formatDuration(p.duration)}</span>
          </div>
          <div className="mt-4 flex items-center justify-center gap-3">
            <Button size="icon" variant="ghost" className={p.shuffle ? "text-accent" : ""} onClick={() => p.control("shuffle")}><Shuffle /></Button>
            <Button size="icon" variant="ghost" onClick={() => p.control("previous")}><SkipBack /></Button>
            <Button size="lg" className="h-14 w-14" onClick={() => p.control(p.playing ? "pause" : "resume")}>
              {p.playing ? <Pause className="fill-current" /> : <Play className="fill-current" />}
            </Button>
            <Button size="icon" variant="ghost" onClick={() => p.control("skip")}><SkipForward /></Button>
            <Button size="icon" variant="ghost" className={p.repeat !== "off" ? "text-accent" : ""} onClick={() => p.control("repeat", { mode: nextRepeat(p.repeat) })}><RepeatIcon /></Button>
          </div>
          <div className="mt-4 space-y-3">
            <label className="flex items-center justify-between text-sm">
              Speed {p.playbackRate.toFixed(2)}×
              <span className="w-40">
                <Slider min={50} max={200} step={5} value={[p.playbackRate * 100]} onValueChange={([v]) => p.setPlaybackRate((v || 100) / 100)} />
              </span>
            </label>
            <label className="flex items-center justify-between text-sm">
              Visualizer
              <Switch checked={p.visualizer} onCheckedChange={p.setVisualizer} />
            </label>
            <label className="flex items-center justify-between text-sm">
              Stop after current
              <Switch checked={p.stopAfterCurrent} onCheckedChange={p.setStopAfterCurrent} />
            </label>
            <div className="flex flex-wrap gap-2">
              {[5, 15, 30, 60].map((m) => (
                <Button key={m} size="sm" variant="secondary" onClick={() => p.setSleep(m)}>Sleep {m}m</Button>
              ))}
              <Button size="sm" variant="secondary" onClick={() => p.setSleep(0)}>After current</Button>
              <Button size="sm" variant="ghost" onClick={() => p.setSleep(null)}>Clear timer</Button>
            </div>
          </div>
          <div className="mt-4 flex justify-center gap-2">
            {t && (
              <Button variant="secondary" onClick={() => api.post("/api/v1/favourites", { type: "track", id: t.id, on: true }).then(() => toast.success("Favourited"))}>
                <Heart className="mr-2" /> Favourite
              </Button>
            )}
            <Button variant="secondary" onClick={() => { ui.openQueue(); ui.set({ nowPlayingOpen: false }); }}>
              <ListMusic className="mr-2" /> Queue
            </Button>
            <Button variant={p.stopAfterCurrent ? "default" : "secondary"} onClick={() => p.setStopAfterCurrent(!p.stopAfterCurrent)}>
              <Square className="mr-2" /> Stop after
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
