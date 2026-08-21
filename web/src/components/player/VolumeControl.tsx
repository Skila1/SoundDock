import { useEffect, useRef, useState } from "react";
import { Volume1, Volume2, VolumeX } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Tooltip } from "@/components/ui/tooltip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { usePlayer } from "@/stores/player";
import { listAudioOutputs } from "@/components/player/audioEngine";

export function VolumeControl() {
  const volume = usePlayer((s) => s.volume);
  const muted = usePlayer((s) => s.muted);
  const sinkId = usePlayer((s) => s.sinkId);
  const setVolume = usePlayer((s) => s.setVolume);
  const toggleMute = usePlayer((s) => s.toggleMute);
  const setSink = usePlayer((s) => s.setSink);
  const wrap = useRef<HTMLDivElement>(null);
  const shown = muted ? 0 : volume;
  const pct = Math.round(shown * 100);
  const Icon = muted || shown <= 0 ? VolumeX : shown < 0.5 ? Volume1 : Volume2;
  const [sinks, setSinks] = useState<{ deviceId: string; label: string }[]>([]);

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.deltaY === 0) return;
      const step = e.deltaMode === WheelEvent.DOM_DELTA_LINE ? 0.05 : Math.min(0.08, Math.max(0.02, Math.abs(e.deltaY) * 0.0008));
      const { volume, setVolume } = usePlayer.getState();
      setVolume(volume + (e.deltaY < 0 ? step : -step));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  useEffect(() => {
    listAudioOutputs().then(setSinks).catch(() => setSinks([]));
  }, []);

  return (
    <div ref={wrap} className="hidden items-center gap-1 md:flex">
      <Tooltip label={muted ? "Unmute" : `Volume ${pct}%`}>
        <Button size="icon" variant="ghost" onClick={toggleMute} aria-label={muted ? "Unmute" : "Mute"}>
          <Icon />
        </Button>
      </Tooltip>
      <div className="w-24">
        <Slider value={[shown * 100]} onValueChange={([v]) => setVolume((v || 0) / 100)} />
      </div>
      {sinks.length > 1 && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="ghost" className="hidden h-8 max-w-[88px] truncate px-2 text-[10px] lg:inline-flex" aria-label="Audio output device">
              {sinks.find((s) => s.deviceId === sinkId)?.label || "System"}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem onSelect={() => setSink("")}>System default</DropdownMenuItem>
            {sinks.map((s) => (
              <DropdownMenuItem key={s.deviceId} onSelect={() => setSink(s.deviceId)}>
                {s.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
