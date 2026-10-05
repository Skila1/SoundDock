import { cn } from "@/lib/utils";

/** The SoundDock wordmark, or just the hexagon mark where space is tight. */
export function Logo({
  className,
  alt = "SoundDock",
  mark = false
}: {
  className?: string;
  alt?: string;
  mark?: boolean;
}) {
  return (
    <img
      src={mark ? "/logo-mark.png" : "/logo-full.png"}
      alt={alt}
      draggable={false}
      className={cn("select-none object-contain", className)}
    />
  );
}
