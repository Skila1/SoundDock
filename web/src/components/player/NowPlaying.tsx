import { useQuery } from "@tanstack/react-query";
import { Heart, ListMusic, Pause, Play, Repeat, Repeat1, Shuffle, SkipBack, SkipForward, Square, X } from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
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
  const progress = p.duration ? (p.position / p.duration) * 100 : 0;
  const RepeatIcon = p.repeat === "one" ? Repeat1 : Repeat;
  const lyricsQ = useQuery({
    queryKey: lyricsQueryKey(t?.id || ""),
    queryFn: () => api.get<TrackLyrics>(`/api/v1/tracks/${encodeURIComponent(t!.id)}/lyrics`),
    enabled: ui.nowPlayingOpen && Boolean(t?.id),
    staleTime: 10 * 60_000
  });
  const lyrics = lyricsQ.data ?? null;
  const showLyrics = Boolean(lyrics && (karaokeLines(lyrics).length || hasPlainBody(lyrics)));

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
          {showLyrics && lyrics && (
            <div className="mt-4">
              <LyricsKaraoke lyrics={lyrics} positionMs={p.position} compact onSeek={(ms) => p.seek(ms)} />
              <Button size="sm" variant="ghost" className="mt-2" onClick={() => ui.openLyrics()}>
                Open lyrics
              </Button>
            </div>
          )}
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
