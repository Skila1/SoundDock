import { Sheet, SheetContent } from "@/components/ui/sheet";
import { useUi } from "@/stores/ui";
import { QueuePanel, QueuePresence } from "./QueuePanel";

export function QueueSheet() {
  const ui = useUi();
  return (
    <Sheet open={ui.queueOpen} onOpenChange={(v) => ui.set({ queueOpen: v })}>
      <SheetContent side={typeof window !== "undefined" && window.innerWidth < 768 ? "bottom" : "right"} className="flex flex-col p-0" hideClose>
        <div className="flex min-h-0 flex-1 flex-col">
          <QueuePresence className="px-4 pt-3" />
          <QueuePanel onClose={() => ui.set({ queueOpen: false })} showPresence={false} />
        </div>
      </SheetContent>
    </Sheet>
  );
}
