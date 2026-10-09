import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "@radix-ui/react-slot";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex select-none items-center justify-center gap-2 whitespace-nowrap rounded-full text-sm font-semibold transition-[background-color,color,box-shadow,transform] duration-150 active:scale-[0.97] disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-accent text-[#04140a] shadow-[0_6px_18px_-8px_var(--sd-accent)] hover:bg-accent-hover",
        secondary: "bg-surface-2 text-foreground ring-1 ring-inset ring-border hover:bg-surface-3",
        ghost: "bg-transparent text-muted hover:bg-surface-2 hover:text-foreground",
        outline: "border border-border bg-transparent hover:bg-surface-2",
        destructive: "bg-destructive text-white hover:opacity-90"
      },
      size: {
        default: "h-10 px-4",
        sm: "h-8 px-3 text-xs",
        lg: "h-11 px-5",
        icon: "h-10 w-10 p-0"
      }
    },
    defaultVariants: { variant: "default", size: "default" }
  }
);

export function Button({
  className,
  variant,
  size,
  asChild,
  ...props
}: React.ComponentProps<"button"> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot : "button";
  return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
