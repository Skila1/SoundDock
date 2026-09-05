import type { ComponentProps } from "react";
import * as Ctx from "@radix-ui/react-context-menu";
import { cn } from "@/lib/utils";

export const ContextMenu = Ctx.Root;
export const ContextMenuTrigger = Ctx.Trigger;

export function ContextMenuContent({ className, onCloseAutoFocus, ...props }: ComponentProps<typeof Ctx.Content>) {
  return (
    <Ctx.Portal>
      <Ctx.Content
        className={cn("z-50 min-w-48 rounded-lg border border-border bg-surface-1 p-1 shadow-card", className)}
        onCloseAutoFocus={(e) => {
          e.preventDefault();
          onCloseAutoFocus?.(e);
        }}
        {...props}
      />
    </Ctx.Portal>
  );
}

export function ContextMenuItem({ className, ...props }: ComponentProps<typeof Ctx.Item>) {
  return <Ctx.Item className={cn("flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-surface-2", className)} {...props} />;
}
