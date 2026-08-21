import { useEffect, useRef, useState } from "react";
import { Volume1, Volume2, VolumeX } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Tooltip } from "@/components/ui/tooltip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { usePlayer } from "@/stores/player";
import { listAudioOutputs } from "@/components/player/audioEngine";

function bumpVolume(deltaY: number) {
  const delta = deltaY || 0;
  if (delta === 0) return;
  const s = usePlayer.getState();
  const current = s.muted ? 0 : s.volume;
  const step = 0.05;
  s.setVolume(current + (delta < 0 ? step : -step));
}

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
      e.stopImmediatePropagation();
      bumpVolume(e.deltaY !== 0 ? e.deltaY : e.deltaX);
    };
    el.addEventListener("wheel", onWheel, { passive: false, capture: true });
    return () => el.removeEventListener("wheel", onWheel, { capture: true });
  }, []);

  useEffect(() => {
    listAudioOutputs().then(setSinks).catch(() => setSinks([]));
  }, []);

  return (
    <div
      ref={wrap}
      className="hidden items-center gap-1 rounded-xl px-2.5 py-1.5 transition-colors hover:bg-white/15 md:flex"
      style={{ overscrollBehavior: "contain" }}
    >
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
