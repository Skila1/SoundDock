import { CircleHelp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { SOUNDDOCK_DISCORD_INVITE } from "@/lib/community";
import { cn } from "@/lib/utils";

function DiscordMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path
        fill="currentColor"
        d="M19.27 5.33A17.3 17.3 0 0 0 14.89 4c-.2.36-.43.85-.59 1.23a16.2 16.2 0 0 0-4.6 0A11.2 11.2 0 0 0 9.1 4a17.4 17.4 0 0 0-4.4 1.34C1.98 9.05 1.3 12.67 1.47 16.24A17.5 17.5 0 0 0 6.8 20c.36-.5.68-1.02.96-1.57a11.4 11.4 0 0 1-1.51-.74c.13-.1.25-.2.37-.3 2.92 1.36 6.08 1.36 8.97 0 .12.1.24.2.37.3-.48.28-.99.53-1.52.74.28.55.6 1.07.96 1.57a17.4 17.4 0 0 0 5.34-3.76c.4-4.13-.68-7.72-2.84-10.91ZM8.7 14.6c-.88 0-1.6-.82-1.6-1.82s.7-1.82 1.6-1.82 1.62.83 1.6 1.82c0 1-.71 1.82-1.6 1.82Zm6.6 0c-.88 0-1.6-.82-1.6-1.82s.7-1.82 1.6-1.82 1.62.83 1.6 1.82c0 1-.7 1.82-1.6 1.82Z"
      />
    </svg>
  );
}

type LinkButtonProps = {
  className?: string;
  variant?: "default" | "secondary" | "ghost" | "outline";
  size?: "default" | "sm" | "lg" | "icon";
  iconOnly?: boolean;
};

export function HelpButton({ className, variant = "ghost", size = "sm", iconOnly }: LinkButtonProps) {
  return (
    <Button asChild variant={variant} size={iconOnly ? "icon" : size} className={className}>
      <a href={SOUNDDOCK_DISCORD_INVITE} target="_blank" rel="noopener noreferrer" aria-label="Help">
        <CircleHelp className="h-4 w-4" />
        {!iconOnly && "Help"}
      </a>
    </Button>
  );
}

export function DiscordServerButton({ className, variant = "secondary", size = "sm", iconOnly }: LinkButtonProps) {
  return (
    <Button asChild variant={variant} size={iconOnly ? "icon" : size} className={className}>
      <a href={SOUNDDOCK_DISCORD_INVITE} target="_blank" rel="noopener noreferrer" aria-label="Discord server">
        <DiscordMark className="h-4 w-4" />
        {!iconOnly && "Discord server"}
      </a>
    </Button>
  );
}

export function CommunityTextLink({ kind, className }: { kind: "help" | "discord"; className?: string }) {
  return (
    <a
      href={SOUNDDOCK_DISCORD_INVITE}
      target="_blank"
      rel="noopener noreferrer"
      className={cn("block rounded-lg px-3 py-1.5 text-xs text-muted hover:bg-surface-2 hover:text-foreground", className)}
    >
      {kind === "help" ? "Help" : "Discord server"}
    </a>
  );
}
