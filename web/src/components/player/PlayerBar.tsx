import { useEffect, useState } from "react";
import {
  Heart,
  ListMusic,
  Maximize2,
  Minimize2,
  Moon,
  Pause,
  PictureInPicture2,
  Play,
  Repeat,
  Repeat1,
  Shuffle,
  SkipBack,
  SkipForward,
  Square
} from "lucide-react";
import { Artwork } from "@/components/media/Artwork";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Tooltip } from "@/components/ui/tooltip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { VolumeControl } from "@/components/player/VolumeControl";
import { formatDuration, artworkUrl } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import { discordOptionVisible, discordReady } from "@/lib/device";
import { toast } from "sonner";

function nextRepeat(mode: string) {
  if (mode === "off") return "queue";
  if (mode === "queue") return "one";
  return "off";
}

async function openDocumentPip() {
  try {
    const dip = (window as unknown as { documentPictureInPicture?: { requestWindow: (o?: { width?: number; height?: number }) => Promise<Window> } }).documentPictureInPicture;
    if (!dip?.requestWindow) return;
    const w = await dip.requestWindow({ width: 380, height: 96 });
    const style = w.document.createElement("style");
    style.textContent = `body{margin:0;font:13px system-ui,sans-serif;background:#121212;color:#f4f4f4;display:flex;align-items:center;gap:12px;padding:12px}
    button{border:0;background:#1db954;color:#04140a;border-radius:999px;width:36px;height:36px;cursor:pointer;font-weight:700}
    .t{font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .a{opacity:.7;font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}`;
    w.document.head.appendChild(style);
    const root = w.document.createElement("div");
    root.style.cssText = "display:flex;align-items:center;gap:12px;width:100%";
    w.document.body.appendChild(root);
    const esc = (v: string) => v.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c] || c));
    const paint = (s: ReturnType<typeof usePlayer.getState>) => {
      const t = s.current;
      root.innerHTML = `<div style="flex:1;min-width:0"><div class="t">${esc(t?.title || "Nothing playing")}</div><div class="a">${esc(t?.artists?.map((a) => a.name).join(", ") || t?.artist || "")}</div></div>`;
      const b = w.document.createElement("button");
      b.textContent = s.playing ? "❚❚" : "▶";
      b.onclick = () => usePlayer.getState().control(s.playing ? "pause" : "resume");
      root.appendChild(b);
    };
    paint(usePlayer.getState());
    const unsub = usePlayer.subscribe(paint);
    w.addEventListener("pagehide", () => unsub());
  } catch {
    /* PiP failure is non-fatal */
  }
}

export function PlayerBar() {
  const p = usePlayer();
  const ui = useUi();
  const t = p.current;
  const progress = p.duration ? (p.position / p.duration) * 100 : 0;
  const showDiscord = discordOptionVisible(p.voice);
  const discordOn = discordReady(p.voice);
  const tiny = p.tinyMode;
  const RepeatIcon = p.repeat === "one" ? Repeat1 : Repeat;
  const [sleepLeft, setSleepLeft] = useState("");

  useEffect(() => {
    if (!p.sleepUntil) {
      setSleepLeft("");
      return;
    }
    const tick = () => {
      const ms = (p.sleepUntil || 0) - Date.now();
      if (ms <= 0) setSleepLeft("");
      else setSleepLeft(`${Math.ceil(ms / 60000)}m`);
    };
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [p.sleepUntil]);

  return (
    <footer className="grid h-[72px] shrink-0 grid-cols-[1fr_auto] items-center gap-3 overflow-x-hidden border-t border-border bg-surface-1/90 px-3 backdrop-blur md:grid-cols-[minmax(160px,1fr)_minmax(240px,2fr)_minmax(200px,1fr)] md:px-4">
      <button className="flex min-w-0 items-center gap-3 text-left" onClick={() => ui.set({ nowPlayingOpen: true })}>
        <div className="h-12 w-12 shrink-0 overflow-hidden rounded-md bg-surface-2">
          {t && <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" />}
        </div>
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{t?.title || "Nothing playing"}</div>
          <div className="truncate text-xs text-muted">{t?.artists?.map((a) => a.name).join(", ") || t?.artist || ""}</div>
        </div>
        {t && !tiny && (
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
          {!tiny && (
            <Tooltip label="Shuffle">
              <Button size="icon" variant="ghost" className={p.shuffle ? "text-accent" : ""} onClick={() => p.control("shuffle")}>
                <Shuffle />
              </Button>
            </Tooltip>
          )}
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
          {!tiny && (
            <Tooltip label={`Repeat ${p.repeat}`}>
              <Button size="icon" variant="ghost" className={p.repeat !== "off" ? "text-accent" : ""} onClick={() => p.control("repeat", { mode: nextRepeat(p.repeat) })}>
                <RepeatIcon />
              </Button>
            </Tooltip>
          )}
        </div>
        <div className="mt-1 flex w-full max-w-xl items-center gap-2">
          <span className="w-10 text-right text-[10px] text-subtle">{formatDuration(p.position)}</span>
          <Slider value={[progress]} onValueChange={([v]) => p.seek(((v || 0) / 100) * (p.duration || 0))} />
          <span className="w-10 text-[10px] text-subtle">{formatDuration(p.duration)}</span>
        </div>
      </div>

      <div className="flex items-center justify-end gap-1">
        {showDiscord && (
          <div className="mr-1 hidden items-center rounded-full bg-surface-2 p-0.5 text-[10px] font-semibold sm:flex" role="group" aria-label="Output">
            <button
              type="button"
              className={`rounded-full px-2 py-1 ${p.output === "browser" ? "bg-surface-1 text-foreground" : "text-muted"}`}
              onClick={() => p.setOutput("browser")}
            >
              Browser
            </button>
            <button
              type="button"
              disabled={!discordOn}
              title={!discordOn ? "Join a Discord voice channel" : "Play in Discord"}
              className={`rounded-full px-2 py-1 disabled:cursor-not-allowed disabled:opacity-40 ${p.output === "discord" ? "bg-surface-1 text-foreground" : "text-muted"}`}
              onClick={() => discordOn && p.setOutput("discord")}
            >
              Discord
            </button>
          </div>
        )}
        <Button size="icon" variant="ghost" className="md:hidden" onClick={() => p.control(p.playing ? "pause" : "resume")}>
          {p.playing ? <Pause /> : <Play />}
        </Button>
        <Tooltip label="Queue">
          <Button
            size="icon"
            variant="ghost"
            className={ui.queuePinned || !ui.queueCollapsed || ui.queueOpen ? "text-accent" : ""}
            onClick={() => ui.toggleQueue()}
            aria-label="Queue"
          >
            <ListMusic />
          </Button>
        </Tooltip>
        {!tiny && (
          <Tooltip label="Now playing">
            <Button size="icon" variant="ghost" className="hidden md:inline-flex" onClick={() => ui.set({ nowPlayingOpen: true })}>
              <Maximize2 />
            </Button>
          </Tooltip>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" aria-label="Player options" className="hidden md:inline-flex">
              <Moon className={p.sleepUntil || p.stopAfterCurrent ? "text-accent" : ""} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem onSelect={() => p.setSleep(5)}>Sleep 5 min{sleepLeft ? ` · ${sleepLeft}` : ""}</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => p.setSleep(15)}>Sleep 15 min</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => p.setSleep(30)}>Sleep 30 min</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => p.setSleep(60)}>Sleep 60 min</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => p.setSleep(0)}>Sleep after current</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => p.setSleep(null)}>Clear sleep timer</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => p.setStopAfterCurrent(!p.stopAfterCurrent)}>
              <Square className="h-3.5 w-3.5" /> {p.stopAfterCurrent ? "Cancel stop after current" : "Stop after current"}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => openDocumentPip()}>
              <PictureInPicture2 className="h-3.5 w-3.5" /> Document PiP
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => p.setTinyMode(!p.tinyMode)}>
              <Minimize2 className="h-3.5 w-3.5" /> {p.tinyMode ? "Full player bar" : "Tiny mode"}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <VolumeControl />
      </div>
    </footer>
  );
}
