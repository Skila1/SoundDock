import { Sheet, SheetContent } from "@/components/ui/sheet";
import { useUi } from "@/stores/ui";
import { QueuePanel } from "./QueuePanel";

export function QueueSheet() {
  const ui = useUi();
  return (
    <Sheet open={ui.queueOpen} onOpenChange={(v) => ui.set({ queueOpen: v })}>
      <SheetContent title="Queue" side={typeof window !== "undefined" && window.innerWidth < 768 ? "bottom" : "right"} className="p-0">
        <QueuePanel onClose={() => ui.set({ queueOpen: false })} />
      </SheetContent>
    </Sheet>
  );
}
