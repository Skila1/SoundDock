import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ChevronDown, Heart, ListMusic, Mic2, Pause, Play, Repeat, Repeat1, Shuffle, SkipBack, SkipForward, SlidersHorizontal, Square } from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl, formatDuration } from "@/lib/utils";
import { Visualizer } from "@/components/player/Visualizer";
import { LyricsKaraoke } from "@/components/player/LyricsView";
import { activeLyricIndex, hasPlainBody, karaokeLines, lyricsQueryKey } from "@/components/player/lyricsSync";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import { toast } from "sonner";
import type { TrackLyrics } from "@/types/api";

export { activeLyricIndex };

function nextRepeat(mode: string) {
  if (mode === "off") return "queue";
  if (mode === "queue") return "one";
  return "off";
}

export function NowPlaying() {
  const ui = useUi();
  const p = usePlayer();
  const t = p.current;
  const [scrub, setScrub] = useState<number | null>(null);
  const pos = scrub ?? Math.min(p.position, p.duration || p.position || 0);
  const progress = p.duration ? Math.min(100, Math.max(0, (pos / p.duration) * 100)) : 0;
  useEffect(() => {
    setScrub(null);
  }, [t?.id]);
  const RepeatIcon = p.repeat === "one" ? Repeat1 : Repeat;
  const lyricsQ = useQuery({
    queryKey: lyricsQueryKey(t?.id || ""),
    queryFn: () => api.get<TrackLyrics>(`/api/v1/tracks/${encodeURIComponent(t!.id)}/lyrics`),
    enabled: ui.nowPlayingOpen && Boolean(t?.id),
    staleTime: 10 * 60_000
  });
  const lyrics = lyricsQ.data ?? null;
  const showLyrics = Boolean(lyrics && (karaokeLines(lyrics).length || hasPlainBody(lyrics)));

  const artist = t?.artists?.map((a) => a.name).join(", ") || t?.artist || "";

  return (
    <Dialog open={ui.nowPlayingOpen} onOpenChange={(v) => ui.set({ nowPlayingOpen: v })}>
      <DialogContent
        hideClose
        overlayClassName="bg-black/80"
        className="sd-full left-0 top-0 h-[100dvh] w-screen max-w-none translate-x-0 translate-y-0 overflow-hidden rounded-none border-0 bg-background p-0 sm:w-screen"
      >
        {t && (
          <div aria-hidden className="pointer-events-none absolute inset-0">
            <div className="absolute inset-0 scale-125 opacity-50 blur-[90px] saturate-150">
              <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="album" />
            </div>
            <div className="absolute inset-0 bg-gradient-to-b from-background/40 via-background/70 to-background" />
          </div>
        )}
        <div className="relative flex h-full flex-col">
          <div className="flex items-center justify-between px-4 pt-[max(1rem,env(safe-area-inset-top))] md:px-8">
            <Button size="icon" variant="ghost" onClick={() => ui.set({ nowPlayingOpen: false })} aria-label="Close">
              <ChevronDown className="!size-6" />
            </Button>
            <div className="text-center">
              <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">Now playing</div>
              {t?.album && <div className="max-w-[60vw] truncate text-sm font-semibold">{t.album}</div>}
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="icon" variant="ghost" aria-label="Playback settings">
                  <SlidersHorizontal className="!size-5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-72 p-3">
                <label className="flex items-center justify-between gap-3 py-1.5 text-sm">
                  <span>Speed <span className="tabular text-muted">{p.playbackRate.toFixed(2)}×</span></span>
                  <span className="w-32">
                    <Slider min={50} max={200} step={5} value={[p.playbackRate * 100]} onValueChange={([v]) => p.setPlaybackRate((v || 100) / 100)} />
                  </span>
                </label>
                <label className="flex items-center justify-between py-1.5 text-sm">
                  Visualizer
                  <Switch checked={p.visualizer} onCheckedChange={p.setVisualizer} />
                </label>
                <label className="flex items-center justify-between py-1.5 text-sm">
                  Stop after current
                  <Switch checked={p.stopAfterCurrent} onCheckedChange={p.setStopAfterCurrent} />
                </label>
                <DropdownMenuSeparator />
                <div className="px-1 pb-1 pt-1 text-[11px] font-semibold uppercase tracking-wider text-subtle">Sleep timer</div>
                <div className="flex flex-wrap gap-1.5">
                  {[5, 15, 30, 60].map((m) => (
                    <Button key={m} size="sm" variant="secondary" onClick={() => p.setSleep(m)}>{m}m</Button>
                  ))}
                  <Button size="sm" variant="secondary" onClick={() => p.setSleep(0)}>End of song</Button>
                  {p.sleepUntil ? <Button size="sm" variant="ghost" onClick={() => p.setSleep(null)}>Clear</Button> : null}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          <div className={`mx-auto grid min-h-0 w-full max-w-6xl flex-1 items-center gap-8 overflow-y-auto px-6 py-6 md:px-10 ${showLyrics ? "lg:grid-cols-2" : ""}`}>
            <div className="mx-auto flex w-full max-w-[min(28rem,52vh)] flex-col">
              <div className="aspect-square w-full overflow-hidden rounded-2xl shadow-[0_40px_90px_-30px_rgba(0,0,0,0.75)] ring-1 ring-inset ring-white/10">
                {t && <Artwork src={artworkUrl("track", t.id, "now")} id={t.id} name={t.title} kind="album" />}
              </div>
              <Visualizer active={p.visualizer && ui.nowPlayingOpen} />
              <div className="mt-6 flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="truncate text-2xl font-extrabold tracking-tight md:text-3xl">{t?.title || "Nothing playing"}</h2>
                  <p className="truncate text-base text-muted">{artist}</p>
                </div>
                {t && (
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label="Favourite"
                    onClick={() => api.post("/api/v1/favourites", { type: "track", id: t.id, on: true }).then(() => toast.success("Favourited"))}
                  >
                    <Heart className="!size-5" />
                  </Button>
                )}
              </div>
              <div className="mt-4">
                <Slider
                  value={[progress]}
                  onValueChange={([v]) => setScrub(((v || 0) / 100) * (p.duration || 0))}
                  onValueCommit={([v]) => {
                    const ms = ((v || 0) / 100) * (p.duration || 0);
                    setScrub(null);
                    p.seek(ms);
                  }}
                />
                <div className="tabular mt-1 flex justify-between text-xs text-subtle">
                  <span>{formatDuration(pos)}</span>
                  <span>{formatDuration(p.duration)}</span>
                </div>
              </div>
              <div className="mt-3 flex items-center justify-between">
                <Button size="icon" variant="ghost" className={p.shuffle ? "text-accent" : ""} onClick={() => p.control("shuffle")} aria-label="Shuffle"><Shuffle className="!size-5" /></Button>
                <Button size="icon" variant="ghost" className="text-foreground" onClick={() => p.control("previous")} aria-label="Previous"><SkipBack className="!size-6 fill-current" /></Button>
                <button
                  type="button"
                  className="flex h-16 w-16 items-center justify-center rounded-full bg-foreground text-background shadow-card transition hover:scale-105 active:scale-95"
                  onClick={() => p.control(p.playing ? "pause" : "resume")}
                  aria-label={p.playing ? "Pause" : "Play"}
                >
                  {p.playing ? <Pause className="h-7 w-7 fill-current" /> : <Play className="ml-1 h-7 w-7 fill-current" />}
                </button>
                <Button size="icon" variant="ghost" className="text-foreground" onClick={() => p.control("skip")} aria-label="Next"><SkipForward className="!size-6 fill-current" /></Button>
                <Button size="icon" variant="ghost" className={p.repeat !== "off" ? "text-accent" : ""} onClick={() => p.control("repeat", { mode: nextRepeat(p.repeat) })} aria-label="Repeat"><RepeatIcon className="!size-5" /></Button>
              </div>
              <div className="mt-4 flex items-center justify-center gap-2">
                <Button variant="ghost" size="sm" onClick={() => { ui.openQueue(); ui.set({ nowPlayingOpen: false }); }}>
                  <ListMusic /> Queue
                </Button>
                {showLyrics && (
                  <Button variant="ghost" size="sm" className="lg:hidden" onClick={() => { ui.openLyrics(); ui.set({ nowPlayingOpen: false }); }}>
                    <Mic2 /> Lyrics
                  </Button>
                )}
                <Button variant="ghost" size="sm" className={p.stopAfterCurrent ? "text-accent" : ""} onClick={() => p.setStopAfterCurrent(!p.stopAfterCurrent)}>
                  <Square /> Stop after
                </Button>
              </div>
            </div>
            {showLyrics && lyrics && (
              <div className="hidden h-full max-h-[70vh] min-h-0 overflow-hidden rounded-2xl bg-surface-1/40 p-6 ring-1 ring-inset ring-border backdrop-blur lg:block">
                <LyricsKaraoke lyrics={lyrics} positionMs={p.position} onSeek={(ms) => p.seek(ms)} />
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
