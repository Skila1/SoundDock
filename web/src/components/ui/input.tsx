import * as React from "react";
import { cn } from "@/lib/utils";

export function Input({ className, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      className={cn(
        "h-10 w-full rounded-lg border border-border bg-surface-1 px-3 text-sm text-foreground outline-none transition placeholder:text-subtle focus:border-accent/60 focus:ring-2 focus:ring-accent/20 focus-visible:outline-none disabled:opacity-60",
        className
      )}
      {...props}
    />
  );
}

export function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      className={cn(
        "min-h-24 w-full rounded-lg border border-border bg-surface-1 px-3 py-2 text-sm text-foreground outline-none transition placeholder:text-subtle focus:border-accent/60 focus:ring-2 focus:ring-accent/20 focus-visible:outline-none disabled:opacity-60",
        className
      )}
      {...props}
    />
  );
}
