import { Heart, MoreHorizontal, Play } from "lucide-react";
import { Link } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useRef, type CSSProperties } from "react";
import { Artwork } from "./Artwork";
import { Button } from "@/components/ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu";
import { formatDuration, artworkUrl, cn } from "@/lib/utils";
import type { Track } from "@/types/api";

export function TrackList({
  tracks,
  onPlay,
  onQueue,
  onNext,
  onFav,
  showAlbum = true,
  currentId
}: {
  tracks: Track[];
  onPlay: (index: number) => void;
  onQueue?: (t: Track) => void;
  onNext?: (t: Track) => void;
  onFav?: (t: Track) => void;
  showAlbum?: boolean;
  currentId?: string;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const virtual = tracks.length > 80;
  const rowVirtualizer = useVirtualizer({
    count: tracks.length,
    getScrollElement: () => parent.current,
    estimateSize: () => 56,
    overscan: 12,
    enabled: virtual
  });

  const row = (t: Track, i: number, style?: CSSProperties) => (
    <ContextMenu key={t.id}>
      <ContextMenuTrigger asChild>
        <div
          style={style}
          className={cn(
            "group grid cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 hover:bg-surface-2",
            showAlbum ? "grid-cols-[32px_1fr_minmax(0,1fr)_64px_40px]" : "grid-cols-[32px_1fr_64px_40px]",
            currentId === t.id && "text-accent"
          )}
          onDoubleClick={() => onPlay(i)}
        >
          <div className="relative text-center text-xs text-subtle">
            <span className="group-hover:hidden">{t.track_number || i + 1}</span>
            <button className="hidden w-full justify-center group-hover:flex" onClick={() => onPlay(i)} aria-label="Play">
              <Play className="h-3.5 w-3.5 fill-current" />
            </button>
          </div>
          <div className="flex min-w-0 items-center gap-3">
            <div className="hidden h-10 w-10 shrink-0 overflow-hidden rounded sm:block">
              <Artwork src={artworkUrl("track", t.id, "thumb")} id={t.id} name={t.title} kind="track" size="sm" />
            </div>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{t.title}</div>
              <div className="truncate text-xs text-muted">{t.artists?.map((a) => a.name).join(", ") || t.artist || ""}</div>
            </div>
          </div>
          {showAlbum && (
            <Link to={t.album_id ? `/albums/${t.album_id}` : "#"} className="hidden truncate text-sm text-muted hover:underline md:block">
              {t.album}
            </Link>
          )}
          <div className="text-right text-xs text-subtle">{formatDuration(t.duration_ms)}</div>
          <div className="flex justify-end gap-1 opacity-0 group-hover:opacity-100">
            {onFav && (
              <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => onFav(t)} aria-label="Favourite">
                <Heart className="h-3.5 w-3.5" />
              </Button>
            )}
            <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="More">
              <MoreHorizontal className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onSelect={() => onPlay(i)}>Play</ContextMenuItem>
        {onNext && <ContextMenuItem onSelect={() => onNext(t)}>Play next</ContextMenuItem>}
        {onQueue && <ContextMenuItem onSelect={() => onQueue(t)}>Add to queue</ContextMenuItem>}
        {onFav && <ContextMenuItem onSelect={() => onFav(t)}>Favourite</ContextMenuItem>}
      </ContextMenuContent>
    </ContextMenu>
  );

  if (!virtual) return <div>{tracks.map((t, i) => row(t, i))}</div>;
  return (
    <div ref={parent} className="max-h-[70vh] overflow-auto scrollbar-thin">
      <div style={{ height: rowVirtualizer.getTotalSize(), position: "relative" }}>
        {rowVirtualizer.getVirtualItems().map((v) =>
          row(tracks[v.index], v.index, { position: "absolute", top: 0, left: 0, width: "100%", transform: `translateY(${v.start}px)` })
        )}
      </div>
    </div>
  );
}
