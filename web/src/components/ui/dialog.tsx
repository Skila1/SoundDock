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
      <DialogPrimitive.Overlay className={cn("fixed inset-0 z-50 bg-black/60 backdrop-blur-sm", overlayClassName)} />
      <DialogPrimitive.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-50 w-[min(520px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-border bg-surface-1 p-5 shadow-card",
          className
        )}
      >
        {title ? (
          <DialogPrimitive.Title className="mb-4 text-lg font-semibold">{title}</DialogPrimitive.Title>
        ) : (
          <DialogPrimitive.Title className="sr-only">Dialog</DialogPrimitive.Title>
        )}
        {!hideClose && (
          <DialogPrimitive.Close className="absolute right-3 top-3 rounded-full p-1 text-muted hover:bg-surface-2" aria-label="Close">
            <X className="h-4 w-4" />
          </DialogPrimitive.Close>
        )}
        {children}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
