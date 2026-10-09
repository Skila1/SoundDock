import type { ReactNode } from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export function DialogContent({ className, overlayClassName, children, title, hideClose }: { className?: string; overlayClassName?: string; children: ReactNode; title?: string; hideClose?: boolean }) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className={cn("sd-overlay fixed inset-0 z-[80] bg-black/60 backdrop-blur-sm", overlayClassName)} />
      <DialogPrimitive.Content
        className={cn(
          "sd-dialog fixed left-1/2 top-1/2 z-[80] w-[min(520px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-surface-1 p-6 shadow-card",
          className
        )}
      >
        {title ? (
          <DialogPrimitive.Title className="mb-5 pr-8 text-lg font-semibold tracking-tight">{title}</DialogPrimitive.Title>
        ) : (
          <DialogPrimitive.Title className="sr-only">Dialog</DialogPrimitive.Title>
        )}
        {!hideClose && (
          <DialogPrimitive.Close className="absolute right-4 top-4 rounded-full p-1.5 text-muted transition hover:bg-surface-2 hover:text-foreground" aria-label="Close">
            <X className="h-4 w-4" />
          </DialogPrimitive.Close>
        )}
        {children}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
