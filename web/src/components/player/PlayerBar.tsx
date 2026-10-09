import { useEffect, useState } from "react";
import {
  Heart,
  ListMusic,
  Maximize2,
  Mic2,
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
  Square,
  Loader2
} from "lucide-react";
import { Link } from "react-router-dom";
import { Artwork } from "@/components/media/Artwork";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Tooltip } from "@/components/ui/tooltip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { VolumeControl } from "@/components/player/VolumeControl";
import { cn, formatDuration, artworkUrl } from "@/lib/utils";
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
  const [scrub, setScrub] = useState<number | null>(null);
  const pos = scrub ?? Math.min(p.position, p.duration || p.position || 0);

  useEffect(() => {
    setScrub(null);
  }, [t?.id]);
  const progress = p.duration ? Math.min(100, Math.max(0, (pos / p.duration) * 100)) : 0;
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
    <footer className="group/player relative h-[80px] shrink-0 overflow-hidden border-t border-border bg-surface-1">
      {t && (
        <div aria-hidden className="pointer-events-none absolute inset-0 opacity-[0.18] blur-3xl saturate-150">
          <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" />
        </div>
      )}
      <div className="pointer-events-none absolute inset-x-0 top-0 h-[2px] bg-surface-3 md:hidden">
        <div className="h-full bg-accent transition-[width] duration-300" style={{ width: `${progress}%` }} />
      </div>
      <div className="relative grid h-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-3 md:grid-cols-[minmax(180px,1fr)_minmax(280px,1.6fr)_minmax(220px,1fr)] md:px-4">
      <div className="flex min-w-0 items-center gap-3">
        <button
          type="button"
          className="relative h-14 w-14 shrink-0 overflow-hidden rounded-lg bg-surface-2 shadow-card ring-1 ring-inset ring-border transition hover:scale-[1.03]"
          onClick={() => ui.set({ nowPlayingOpen: true })}
          aria-label="Open now playing"
        >
          {t && <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" />}
        </button>
        <div className="min-w-0">
          {t ? (
            <Link to={`/tracks/${t.id}`} className="block truncate text-sm font-semibold hover:underline">
              {t.title}
            </Link>
          ) : (
            <div className="truncate text-sm font-semibold text-muted">Nothing playing</div>
          )}
          <div className="truncate text-xs text-muted">{t?.artists?.map((a) => a.name).join(", ") || t?.artist || ""}</div>
        </div>
        {t && !tiny && (
          <Tooltip label="Favourite">
            <Button
              size="icon"
              variant="ghost"
              className="hidden h-8 w-8 shrink-0 md:inline-flex"
              aria-label="Favourite"
              onClick={() => {
                api.post("/api/v1/favourites", { type: "track", id: t.id, on: true }).then(() => toast.success("Added to favourites"));
              }}
            >
              <Heart className="h-4 w-4" />
            </Button>
          </Tooltip>
        )}
      </div>

      <div className="hidden flex-col items-center gap-1 md:flex">
        <div className="flex items-center gap-2">
          {!tiny && (
            <Tooltip label={p.shuffle ? "Shuffle on" : "Shuffle"}>
              <Button size="icon" variant="ghost" className={cn("h-8 w-8", p.shuffle && "text-accent")} onClick={() => p.control("shuffle")} aria-label="Shuffle" aria-pressed={p.shuffle}>
                <Shuffle />
              </Button>
            </Tooltip>
          )}
          <Tooltip label="Previous">
            <Button size="icon" variant="ghost" className="h-9 w-9 text-foreground" onClick={() => p.control("previous")} aria-label="Previous">
              <SkipBack className="fill-current" />
            </Button>
          </Tooltip>
          <Tooltip label={p.playing ? "Pause" : "Play"}>
            <button
              type="button"
              className="flex h-10 w-10 items-center justify-center rounded-full bg-foreground text-background shadow-card transition hover:scale-105 active:scale-95"
              onClick={() => p.control(p.playing ? "pause" : "resume")}
              aria-label={p.playing ? "Pause" : "Play"}
            >
              {p.playing ? <Pause className="h-4 w-4 fill-current" /> : <Play className="ml-0.5 h-4 w-4 fill-current" />}
            </button>
          </Tooltip>
          <Tooltip label="Next">
            <Button size="icon" variant="ghost" className="h-9 w-9 text-foreground" onClick={() => p.control("skip")} aria-label="Next">
              <SkipForward className="fill-current" />
            </Button>
          </Tooltip>
          {!tiny && (
            <Tooltip label={`Repeat ${p.repeat}`}>
              <Button size="icon" variant="ghost" className={cn("h-8 w-8", p.repeat !== "off" && "text-accent")} onClick={() => p.control("repeat", { mode: nextRepeat(p.repeat) })} aria-label="Repeat">
                <RepeatIcon />
              </Button>
            </Tooltip>
          )}
        </div>
        <div className="flex w-full max-w-2xl items-center gap-2">
          <span className="tabular w-10 text-right text-[11px] text-subtle">{formatDuration(pos)}</span>
          <Slider
            className="sd-seek"
            value={[progress]}
            onValueChange={([v]) => setScrub(((v || 0) / 100) * (p.duration || 0))}
            onValueCommit={([v]) => {
              const ms = ((v || 0) / 100) * (p.duration || 0);
              setScrub(null);
              p.seek(ms);
            }}
          />
          <span className="tabular w-10 text-[11px] text-subtle">{formatDuration(p.duration)}</span>
        </div>
      </div>

      <div className="flex items-center justify-end gap-1">
        {showDiscord && (
          <div className="mr-1 hidden items-center rounded-full bg-surface-2/80 p-0.5 text-[11px] font-semibold ring-1 ring-inset ring-border sm:flex" role="group" aria-label="Output">
            <button
              type="button"
              aria-pressed={p.output === "browser"}
              className={`rounded-full px-2.5 py-1 transition-colors ${p.output === "browser" ? "bg-surface-1 text-foreground shadow-sm" : "text-muted hover:text-foreground"}`}
              onClick={() => p.setOutput("browser")}
            >
              Browser
            </button>
            <button
              type="button"
              disabled={p.discordJoining}
              aria-pressed={p.output === "discord" || p.discordJoining}
              aria-busy={p.discordJoining}
              title={p.discordJoining ? "Joining your voice channel…" : !discordOn ? "Join a Discord voice channel, then click to play there" : "Play in Discord"}
              className={`inline-flex items-center gap-1 rounded-full px-2 py-1 disabled:cursor-wait ${p.output === "discord" || p.discordJoining ? "bg-surface-1 text-foreground" : "text-muted"} ${!discordOn && !p.discordJoining ? "opacity-60" : ""}`}
              onClick={() => void p.setOutput("discord")}
            >
              {p.discordJoining && <Loader2 className="h-3 w-3 animate-spin" />}
              {p.discordJoining ? "Joining…" : "Discord"}
            </button>
          </div>
        )}
        <Button size="icon" variant="ghost" className="md:hidden" onClick={() => p.control("previous")} aria-label="Previous">
          <SkipBack />
        </Button>
        <Button size="icon" variant="ghost" className="md:hidden" onClick={() => p.control(p.playing ? "pause" : "resume")} aria-label={p.playing ? "Pause" : "Play"}>
          {p.playing ? <Pause /> : <Play />}
        </Button>
        <Button size="icon" variant="ghost" className="md:hidden" onClick={() => p.control("skip")} aria-label="Next">
          <SkipForward />
        </Button>
        <Tooltip label="Lyrics">
          <Button
            size="icon"
            variant="ghost"
            className={cn("hidden sm:inline-flex", ui.lyricsOpen && "text-accent")}
            onClick={() => ui.toggleLyrics()}
            aria-label="Lyrics"
            aria-pressed={ui.lyricsOpen}
          >
            <Mic2 />
          </Button>
        </Tooltip>
        <Tooltip label="Queue">
          <Button
            size="icon"
            variant="ghost"
            className={!ui.lyricsOpen && (ui.queuePinned || !ui.queueCollapsed || ui.queueOpen) ? "text-accent" : ""}
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
      </div>
    </footer>
  );
}
