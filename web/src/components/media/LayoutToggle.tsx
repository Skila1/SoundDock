import { LayoutGrid, List } from "lucide-react";
import { cn } from "@/lib/utils";
import { useUi } from "@/stores/ui";

export function LayoutToggle() {
  const layout = useUi((s) => s.libraryLayout);
  const set = useUi((s) => s.set);
  return (
    <div className="flex rounded-lg bg-surface-2 p-0.5">
      <button
        type="button"
        className={cn("rounded-md p-1.5", layout === "grid" ? "bg-surface-3 text-foreground" : "text-muted hover:text-foreground")}
        aria-label="Album grid"
        onClick={() => set({ libraryLayout: "grid" })}
      >
        <LayoutGrid className="h-4 w-4" />
      </button>
      <button
        type="button"
        className={cn("rounded-md p-1.5", layout === "list" ? "bg-surface-3 text-foreground" : "text-muted hover:text-foreground")}
        aria-label="Detailed list"
        onClick={() => set({ libraryLayout: "list" })}
      >
        <List className="h-4 w-4" />
      </button>
    </div>
  );
}
