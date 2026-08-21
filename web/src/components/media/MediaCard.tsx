import type { ReactNode } from "react";
import { Play } from "lucide-react";
import { Link } from "react-router-dom";
import { Artwork } from "./Artwork";
import { cn, artworkUrl } from "@/lib/utils";
import { Button } from "@/components/ui/button";

export function MediaCard({
  to,
  id,
  title,
  subtitle,
  kind,
  onPlay,
  className
}: {
  to: string;
  id: string;
  title: string;
  subtitle?: string;
  kind: "album" | "artist" | "playlist" | "track";
  onPlay?: () => void;
  className?: string;
}) {
  const src = kind === "track" ? artworkUrl("track", id, "card") : artworkUrl(kind, id, "card");
  return (
    <Link to={to} className={cn("group block min-w-[148px] max-w-[180px]", className)}>
      <div className="relative overflow-hidden rounded-lg bg-surface-2 shadow-card">
        <div className="aspect-square">
          <Artwork src={src} id={id} name={title} kind={kind} />
        </div>
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
