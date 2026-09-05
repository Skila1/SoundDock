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
      className={cn("relative flex h-5 w-full touch-none items-center", className)}
      value={value}
      onValueChange={onValueChange}
      onValueCommit={onValueCommit}
      max={max}
      min={min}
      step={step}
    >
      <SliderPrimitive.Track className="relative h-1 grow rounded-full bg-surface-3">
        <SliderPrimitive.Range className="absolute h-full rounded-full bg-accent" />
      </SliderPrimitive.Track>
      <SliderPrimitive.Thumb className="block h-3.5 w-3.5 rounded-full bg-foreground shadow" />
    </SliderPrimitive.Root>
  );
}
