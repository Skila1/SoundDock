import { useState } from "react";
import { Disc3, Mic2, ListMusic, Music } from "lucide-react";
import { cn, colorFromId, initials } from "@/lib/utils";

const icons = { album: Disc3, artist: Mic2, playlist: ListMusic, track: Music };

export function Artwork({
  src,
  alt,
  id = "x",
  name = "?",
  kind = "album",
  size = "md",
  className,
  rounded
}: {
  src?: string;
  alt?: string;
  id?: string;
  name?: string;
  kind?: keyof typeof icons;
  size?: "sm" | "md" | "lg" | "hero";
  className?: string;
  rounded?: "square" | "full";
}) {
  const [failed, setFailed] = useState(false);
  const dims = { sm: "h-10 w-10", md: "h-full w-full", lg: "h-40 w-40", hero: "h-full w-full" };
  const Icon = icons[kind];
  const round = rounded === "full" || kind === "artist" ? "rounded-full" : "rounded-md";
  if (!src || failed) {
    return (
      <div
        className={cn("flex items-center justify-center text-white/80", dims[size], round, className)}
        style={{ background: colorFromId(id) }}
        aria-hidden
      >
        <span className="flex flex-col items-center gap-1">
          <Icon className="h-1/3 w-1/3 opacity-80" />
          {size !== "sm" && <span className="text-[10px] font-semibold tracking-wide">{initials(name)}</span>}
        </span>
      </div>
    );
  }
  return (
    <img
      src={src}
      alt={alt || name}
      loading="lazy"
      onError={() => setFailed(true)}
      className={cn("object-cover", dims[size], round, className)}
    />
  );
}
