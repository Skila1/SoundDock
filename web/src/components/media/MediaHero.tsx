import type { ReactNode } from "react";
import { Loader2, MoreHorizontal, Pause, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip } from "@/components/ui/tooltip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { useArtworkVersion, withArtworkVersion } from "@/stores/artwork";

export type HeroMenuItem =
  | { label: string; icon?: ReactNode; onSelect: () => void; destructive?: boolean; hidden?: boolean }
  | "separator";

/**
 * The header every collection page shares: artwork, eyebrow, title, details,
 * a big play button, a few icon actions and a "more" menu for the rest. The
 * artwork also tints a soft backdrop behind the header.
 */
export function MediaHero({
  art,
  backdrop,
  eyebrow,
  title,
  meta,
  stats,
  description,
  round,
  onPlay,
  playing,
  busy,
  playDisabled,
  actions,
  menu
}: {
  art: ReactNode;
  backdrop?: string;
  eyebrow?: ReactNode;
  title: string;
  meta?: ReactNode;
  stats?: ReactNode;
  description?: ReactNode;
  round?: boolean;
  onPlay?: () => void;
  playing?: boolean;
  busy?: boolean;
  playDisabled?: boolean;
  actions?: ReactNode;
  menu?: HeroMenuItem[];
}) {
  const version = useArtworkVersion((s) => s.version);
  const bg = withArtworkVersion(backdrop, version);
  const items = (menu || []).filter((m) => m === "separator" || !m.hidden);
  const hasMenu = items.some((m) => m !== "separator");
  return (
    <header className="relative -mx-4 -mt-6 mb-8 overflow-hidden px-4 pb-6 pt-10 md:-mx-8 md:-mt-8 md:px-8 md:pt-14">
      {bg && (
        <div aria-hidden className="pointer-events-none absolute inset-0">
          <img src={bg} alt="" className="h-full w-full scale-125 object-cover opacity-40 blur-3xl saturate-150" onError={(e) => (e.currentTarget.style.display = "none")} />
          <div className="absolute inset-0 bg-gradient-to-b from-background/20 via-background/70 to-background" />
        </div>
      )}
      <div className="relative flex flex-col gap-6 md:flex-row md:items-end">
        <div
          className={cn(
            "mx-auto h-48 w-48 shrink-0 overflow-hidden bg-surface-2 md:mx-0 shadow-[0_24px_60px_-20px_rgba(0,0,0,0.6)] ring-1 ring-inset ring-border md:h-56 md:w-56",
            round ? "rounded-full" : "rounded-2xl"
          )}
        >
          {art}
        </div>
        <div className="min-w-0 flex-1">
          {eyebrow && <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted">{eyebrow}</p>}
          <h1 className="mt-1.5 line-clamp-2 break-words text-3xl font-extrabold leading-[1.05] tracking-tight md:text-5xl xl:text-6xl">{title}</h1>
          {meta && <div className="mt-3 flex flex-wrap items-center gap-x-1.5 text-sm text-foreground/90">{meta}</div>}
          {stats && <div className="mt-1 text-sm text-muted">{stats}</div>}
          {description && <div className="mt-2 max-w-3xl text-sm text-muted">{description}</div>}
        </div>
      </div>
      <div className="relative mt-6 flex flex-wrap items-center gap-2">
        {onPlay && (
          <Tooltip label={playing ? "Pause" : "Play"}>
            <button
              type="button"
              disabled={playDisabled || busy}
              onClick={onPlay}
              aria-label={playing ? "Pause" : "Play"}
              className="mr-2 flex h-14 w-14 items-center justify-center rounded-full bg-accent text-[#04140a] shadow-[0_10px_30px_-10px_var(--sd-accent)] transition hover:scale-105 hover:bg-accent-hover active:scale-95 disabled:opacity-50 disabled:hover:scale-100"
            >
              {busy ? <Loader2 className="h-6 w-6 animate-spin" /> : playing ? <Pause className="h-6 w-6 fill-current" /> : <Play className="ml-1 h-6 w-6 fill-current" />}
            </button>
          </Tooltip>
        )}
        {actions}
        {hasMenu && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon" variant="ghost" aria-label="More options">
                <MoreHorizontal className="!size-5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="min-w-52">
              {items.map((m, i) =>
                m === "separator" ? (
                  <DropdownMenuSeparator key={`sep-${i}`} />
                ) : (
                  <DropdownMenuItem key={m.label} onSelect={m.onSelect} className={m.destructive ? "text-destructive" : undefined}>
                    {m.icon}
                    {m.label}
                  </DropdownMenuItem>
                )
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </header>
  );
}

/** Round icon button used in the hero action row. */
export function HeroIconButton({ label, onClick, active, disabled, children }: { label: string; onClick: () => void; active?: boolean; disabled?: boolean; children: ReactNode }) {
  return (
    <Tooltip label={label}>
      <Button size="icon" variant="ghost" aria-label={label} aria-pressed={active} disabled={disabled} onClick={onClick} className={cn("[&_svg]:!size-5", active && "text-accent")}>
        {children}
      </Button>
    </Tooltip>
  );
}
