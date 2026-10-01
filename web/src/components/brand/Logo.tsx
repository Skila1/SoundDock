import { cn } from "@/lib/utils";

export function Logo({
  className,
  alt = "SoundDock"
}: {
  className?: string;
  alt?: string;
}) {
  return <img src="/icons/icon-192.png" alt={alt} className={cn("select-none object-contain", className)} />;
}
