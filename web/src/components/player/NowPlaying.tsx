import { Heart, ListMusic, Pause, Play, Repeat, Shuffle, SkipBack, SkipForward, X } from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl, formatDuration } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import { toast } from "sonner";

export function NowPlaying() {
  const ui = useUi();
  const p = usePlayer();
  const t = p.current;
  const progress = p.duration ? (p.position / p.duration) * 100 : 0;
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
          <div className="mt-6">
            <h2 className="text-2xl font-semibold">{t?.title || "Nothing playing"}</h2>
            <p className="text-muted">{t?.artists?.map((a) => a.name).join(", ") || t?.artist}</p>
            <p className="text-sm text-subtle">{t?.album}</p>
          </div>
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
            <Button size="icon" variant="ghost" className={p.repeat !== "off" ? "text-accent" : ""} onClick={() => p.control("repeat", { mode: p.repeat === "queue" ? "off" : "queue" })}><Repeat /></Button>
          </div>
          <div className="mt-4 flex justify-center gap-2">
            {t && (
              <Button variant="secondary" onClick={() => api.post("/api/v1/favourites", { type: "track", id: t.id, on: true }).then(() => toast.success("Favourited"))}>
                <Heart className="mr-2" /> Favourite
              </Button>
            )}
            <Button variant="secondary" onClick={() => ui.set({ queueOpen: true, nowPlayingOpen: false })}>
              <ListMusic className="mr-2" /> Queue
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
