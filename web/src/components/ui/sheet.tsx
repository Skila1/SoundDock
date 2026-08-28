import type { ReactNode } from "react";
import * as SheetPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

export const Sheet = SheetPrimitive.Root;
export const SheetTrigger = SheetPrimitive.Trigger;

export function SheetContent({
  side = "right",
  className,
  children,
  title,
  hideClose
}: {
  side?: "right" | "bottom" | "left";
  className?: string;
  children: ReactNode;
  title?: string;
  hideClose?: boolean;
}) {
  const pos =
    side === "bottom"
      ? "inset-x-0 bottom-0 h-[85vh] rounded-t-xl"
      : side === "left"
        ? "inset-y-0 left-0 w-[min(360px,90vw)]"
        : "inset-y-0 right-0 w-[min(420px,92vw)]";
  return (
    <SheetPrimitive.Portal>
      <SheetPrimitive.Overlay className="fixed inset-0 z-50 bg-black/50" />
      <SheetPrimitive.Content className={cn("fixed z-50 border-border bg-surface-1 p-4 shadow-card", pos, className)}>
        {title ? (
          <SheetPrimitive.Title className="mb-3 text-base font-semibold">{title}</SheetPrimitive.Title>
        ) : (
          <SheetPrimitive.Title className="sr-only">Panel</SheetPrimitive.Title>
        )}
        {!hideClose && (
          <SheetPrimitive.Close className="absolute right-3 top-3 rounded-full p-1 text-muted hover:bg-surface-2" aria-label="Close">
            <X className="h-4 w-4" />
          </SheetPrimitive.Close>
        )}
        {children}
      </SheetPrimitive.Content>
    </SheetPrimitive.Portal>
  );
}
