import type { DragEvent, ReactNode } from "react";
import { Play } from "lucide-react";
import { Link } from "react-router-dom";
import { Artwork } from "./Artwork";
import { cn, artworkUrl } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { api } from "@/lib/api";
import { addTracksToPlaylist, TRACK_DND_MIME } from "./TrackList";
import { toast } from "sonner";

function parseTrackIds(e: DragEvent) {
  const raw = e.dataTransfer.getData(TRACK_DND_MIME) || e.dataTransfer.getData("text/plain");
  if (!raw) return [] as string[];
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed?.track_ids)) return parsed.track_ids.filter((id: unknown) => typeof id === "string");
  } catch {
    /* plain list */
  }
  return raw.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean);
}

export function MediaCard({
  to,
  id,
  title,
  subtitle,
  kind,
  onPlay,
  className,
  explicit,
  codec
}: {
  to: string;
  id: string;
  title: string;
  subtitle?: string;
  kind: "album" | "artist" | "playlist" | "track";
  onPlay?: () => void;
  className?: string;
  explicit?: boolean | null;
  codec?: string;
}) {
  const src = kind === "track" ? artworkUrl("track", id, "card") : artworkUrl(kind, id, "card");
  const onDragOver = (e: DragEvent) => {
    if (kind !== "playlist") return;
    if (![TRACK_DND_MIME, "text/plain"].some((t) => e.dataTransfer.types.includes(t))) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
  };
  const onDrop = async (e: DragEvent) => {
    if (kind !== "playlist") return;
    e.preventDefault();
    const ids = parseTrackIds(e);
    if (!ids.length) return;
    try {
      await addTracksToPlaylist(id, ids);
    } catch {
      toast.error("Could not add to playlist");
    }
  };
  return (
    <Link
      to={to}
      data-playlist-id={kind === "playlist" ? id : undefined}
      onDragOver={onDragOver}
      onDrop={onDrop}
      className={cn("group block min-w-[148px] max-w-[180px]", className)}
    >
      <div className="relative overflow-hidden rounded-lg bg-surface-2 shadow-card">
        <div className="aspect-square">
          <Artwork src={src} id={id} name={title} kind={kind} />
        </div>
        {(explicit || codec) && (
          <div className="absolute left-2 top-2 flex gap-1">
            {explicit && <Badge tone="warning">E</Badge>}
            {codec && <Badge>{codec}</Badge>}
          </div>
        )}
        {onPlay && (
          <Button
            size="icon"
            className="absolute bottom-2 right-2 translate-y-2 opacity-0 shadow-lg transition group-hover:translate-y-0 group-hover:opacity-100"
            onClick={(e) => {
              e.preventDefault();
              onPlay();
            }}
            aria-label={`Play ${title}`}
          >
            <Play className="fill-current" />
          </Button>
        )}
      </div>
      <div className="mt-2 truncate text-sm font-medium">{title}</div>
      {subtitle && <div className="truncate text-xs text-muted">{subtitle}</div>}
    </Link>
  );
}

export function MediaShelf({ title, children, empty }: { title: string; children: ReactNode; empty?: boolean }) {
  if (empty) return null;
  return (
    <section className="mb-8">
      <h2 className="mb-3 text-lg font-semibold">{title}</h2>
      <div className="flex gap-4 overflow-x-auto pb-2 scrollbar-thin">{children}</div>
    </section>
  );
}
