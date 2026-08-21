import { Heart, ListMusic, Maximize2, Pause, Play, Repeat, Shuffle, SkipBack, SkipForward, Volume2, VolumeX } from "lucide-react";
import { Artwork } from "@/components/media/Artwork";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Tooltip } from "@/components/ui/tooltip";
import { formatDuration, artworkUrl } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import { toast } from "sonner";

export function PlayerBar() {
  const p = usePlayer();
  const ui = useUi();
  const t = p.current;
  const progress = p.duration ? (p.position / p.duration) * 100 : 0;

  return (
    <footer className="grid h-[72px] shrink-0 grid-cols-[1fr_auto] items-center gap-3 border-t border-border bg-surface-1/90 px-3 backdrop-blur md:grid-cols-[minmax(180px,1fr)_minmax(280px,2fr)_minmax(180px,1fr)] md:px-4">
      <button className="flex min-w-0 items-center gap-3 text-left" onClick={() => ui.set({ nowPlayingOpen: true })}>
        <div className="h-12 w-12 shrink-0 overflow-hidden rounded-md bg-surface-2">
          {t && <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" />}
        </div>
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{t?.title || "Nothing playing"}</div>
          <div className="truncate text-xs text-muted">{t?.artists?.map((a) => a.name).join(", ") || t?.artist || ""}</div>
        </div>
        {t && (
          <Tooltip label="Favourite">
            <Button
              size="icon"
              variant="ghost"
              className="hidden h-8 w-8 md:inline-flex"
              onClick={(e) => {
                e.stopPropagation();
                api.post("/api/v1/favourites", { type: "track", id: t.id, on: true }).then(() => toast.success("Added to favourites"));
              }}
            >
              <Heart className="h-4 w-4" />
            </Button>
          </Tooltip>
        )}
      </button>

      <div className="hidden flex-col items-center md:flex">
        <div className="flex items-center gap-1">
          <Tooltip label="Shuffle">
            <Button size="icon" variant="ghost" className={p.shuffle ? "text-accent" : ""} onClick={() => p.control("shuffle")}>
              <Shuffle />
            </Button>
          </Tooltip>
          <Tooltip label="Previous">
            <Button size="icon" variant="ghost" onClick={() => p.control("previous")}>
              <SkipBack />
            </Button>
          </Tooltip>
          <Tooltip label={p.playing ? "Pause" : "Play"}>
            <Button size="icon" onClick={() => p.control(p.playing ? "pause" : "resume")} aria-label={p.playing ? "Pause" : "Play"}>
              {p.playing ? <Pause className="fill-current" /> : <Play className="fill-current" />}
            </Button>
          </Tooltip>
          <Tooltip label="Next">
            <Button size="icon" variant="ghost" onClick={() => p.control("skip")}>
              <SkipForward />
            </Button>
          </Tooltip>
          <Tooltip label="Repeat">
            <Button size="icon" variant="ghost" className={p.repeat !== "off" ? "text-accent" : ""} onClick={() => p.control("repeat", { mode: p.repeat === "queue" ? "off" : "queue" })}>
              <Repeat />
            </Button>
          </Tooltip>
        </div>
        <div className="mt-1 flex w-full max-w-xl items-center gap-2">
          <span className="w-10 text-right text-[10px] text-subtle">{formatDuration(p.position)}</span>
          <Slider value={[progress]} onValueChange={([v]) => p.seek(((v || 0) / 100) * (p.duration || 0))} />
          <span className="w-10 text-[10px] text-subtle">{formatDuration(p.duration)}</span>
        </div>
      </div>

      <div className="flex items-center justify-end gap-1">
        <Button size="icon" variant="ghost" className="md:hidden" onClick={() => p.control(p.playing ? "pause" : "resume")}>
          {p.playing ? <Pause /> : <Play />}
        </Button>
        <Tooltip label="Queue">
          <Button
            size="icon"
            variant="ghost"
            onClick={() => {
              if (typeof window !== "undefined" && window.matchMedia("(min-width: 1280px)").matches) {
                ui.set({ queueCollapsed: !ui.queueCollapsed });
              } else {
                ui.set({ queueOpen: true });
              }
            }}
            aria-label="Queue"
          >
            <ListMusic />
          </Button>
        </Tooltip>
        <Tooltip label="Now playing">
          <Button size="icon" variant="ghost" className="hidden md:inline-flex" onClick={() => ui.set({ nowPlayingOpen: true })}>
            <Maximize2 />
          </Button>
        </Tooltip>
        <Button size="icon" variant="ghost" className="hidden md:inline-flex" onClick={p.toggleMute} aria-label="Mute">
          {p.muted ? <VolumeX /> : <Volume2 />}
        </Button>
        <div className="hidden w-24 md:block">
          <Slider value={[p.muted ? 0 : p.volume * 100]} onValueChange={([v]) => p.setVolume((v || 0) / 100)} />
        </div>
      </div>
    </footer>
  );
}
