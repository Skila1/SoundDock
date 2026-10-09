import * as SliderPrimitive from "@radix-ui/react-slider";
import { cn } from "@/lib/utils";

export function Slider({ className, value, onValueChange, onValueCommit, max = 100, min = 0, step = 1 }: {
  className?: string;
  value: number[];
  onValueChange: (v: number[]) => void;
  onValueCommit?: (v: number[]) => void;
  max?: number;
  min?: number;
  step?: number;
}) {
  return (
    <SliderPrimitive.Root
      className={cn("group/slider relative flex h-5 w-full cursor-pointer touch-none select-none items-center", className)}
      value={value}
      onValueChange={onValueChange}
      onValueCommit={onValueCommit}
      max={max}
      min={min}
      step={step}
    >
      <SliderPrimitive.Track className="relative h-1 grow overflow-hidden rounded-full bg-surface-3 transition-[height] group-hover/slider:h-1.5">
        <SliderPrimitive.Range className="absolute h-full rounded-full bg-foreground/80 transition-colors group-hover/slider:bg-accent" />
      </SliderPrimitive.Track>
      <SliderPrimitive.Thumb className="block h-3 w-3 scale-0 rounded-full bg-foreground shadow-md outline-none transition-transform group-hover/slider:scale-100 focus-visible:scale-100 focus-visible:ring-2 focus-visible:ring-accent/40" />
    </SliderPrimitive.Root>
  );
}
