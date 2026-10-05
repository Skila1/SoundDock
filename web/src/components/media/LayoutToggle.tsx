import { LayoutGrid, List } from "lucide-react";
import { cn } from "@/lib/utils";
import { useUi } from "@/stores/ui";

type Layout = "grid" | "list";

export function LayoutToggle({ value, onChange }: { value?: Layout; onChange?: (l: Layout) => void } = {}) {
  const globalLayout = useUi((s) => s.libraryLayout);
  const set = useUi((s) => s.set);
  const layout = value ?? globalLayout;
  const pick = (l: Layout) => {
    if (onChange) onChange(l);
    else set({ libraryLayout: l });
  };
  return (
    <div className="flex h-9 items-center rounded-lg bg-surface-2 p-0.5" role="group" aria-label="Layout">
      <button
        type="button"
        className={cn("rounded-md p-1.5", layout === "grid" ? "bg-surface-3 text-foreground" : "text-muted hover:text-foreground")}
        aria-label="Album grid"
        aria-pressed={layout === "grid"}
        onClick={() => pick("grid")}
      >
        <LayoutGrid className="h-4 w-4" />
      </button>
      <button
        type="button"
        className={cn("rounded-md p-1.5", layout === "list" ? "bg-surface-3 text-foreground" : "text-muted hover:text-foreground")}
        aria-label="Detailed list"
        aria-pressed={layout === "list"}
        onClick={() => pick("list")}
      >
        <List className="h-4 w-4" />
      </button>
    </div>
  );
}
