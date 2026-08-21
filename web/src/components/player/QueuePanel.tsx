import { useRef, useState } from "react";
import { GripVertical, History, ListPlus, PanelRightClose, Play, Trash2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Track } from "@/types/api";
import { toast } from "sonner";
import { Switch } from "@/components/ui/switch";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";

export function QueuePanel({ onClose, onCollapse }: { onClose?: () => void; onCollapse?: () => void }) {
  const p = usePlayer();
  const drag = useRef<number>(-1);
  const [saveOpen, setSaveOpen] = useState(false);
  const [name, setName] = useState("Queue");
  const items = p.queue?.items || [];
  const ids = items.map((i) => i.track_id);
  const counts = ids.reduce((m, id) => m.set(id, (m.get(id) || 0) + 1), new Map<string, number>());
  const { data: tracks } = useQuery({
    queryKey: ["queue-tracks", ids],
    enabled: ids.length > 0,
    queryFn: async () => {
      const out: Track[] = [];
      for (const id of ids.slice(0, 80)) {
        try {
          out.push(await api.get<Track>(`/api/v1/tracks/${id}`));
        } catch {
          out.push({ id, title: id });
        }
      }
      return out;
    }
  });
  const map = new Map((tracks || []).map((t) => [t.id, t]));
  const current = p.queue?.current_index ?? 0;
  const history = items.slice(0, current);
  const upcoming = items.slice(current);

  const row = (item: (typeof items)[number], i: number) => {
    const t = map.get(item.track_id);
    const dup = (counts.get(item.track_id) || 0) > 1;
    return (
      <div
        key={item.id}
        draggable
        onDragStart={() => {
          drag.current = i;
        }}
        onDragOver={(e) => e.preventDefault()}
        onDrop={() => {
          const from = drag.current;
          if (from < 0 || from === i) return;
          p.control("reorder", { from, to: i }).then(() => p.load());
        }}
        className={`flex cursor-grab items-center gap-2 rounded-md p-2 active:cursor-grabbing ${i === current ? "bg-surface-2" : "hover:bg-surface-2"}`}
      >
        <GripVertical className="h-3.5 w-3.5 shrink-0 text-subtle" />
        <div className="h-10 w-10 overflow-hidden rounded">
          <Artwork src={artworkUrl("track", item.track_id, "thumb")} id={item.track_id} name={t?.title} kind="track" size="sm" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <div className="truncate text-sm">{t?.title || "Track"}</div>
            {dup && <Badge tone="warning">Duplicate</Badge>}
          </div>
          <div className="truncate text-xs text-muted">
            {t?.artists?.map((a) => a.name).join(", ") || t?.artist || (i === current ? "Now playing" : i < current ? "Played" : "Up next")}
          </div>
        </div>
        <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => p.playNow(i)} aria-label="Play now">
          <Play className="h-3.5 w-3.5" />
        </Button>
      </div>
    );
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex items-center gap-2">
          <h2 className="font-semibold">Queue</h2>
        </div>
        <div className="flex items-center gap-1">
          {onCollapse && (
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onCollapse} aria-label="Collapse queue">
              <PanelRightClose className="h-4 w-4" />
            </Button>
          )}
          {onClose && (
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onClose} aria-label="Close queue">
              <X className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
      <div className="flex justify-between px-4">
        <span className="text-sm text-muted">{ids.length} tracks</span>
        <div className="flex gap-1">
          <Button size="sm" variant="ghost" onClick={() => setSaveOpen(true)} disabled={!ids.length}>
            <ListPlus className="mr-1 h-3.5 w-3.5" /> Save
          </Button>
          <Button size="sm" variant="ghost" onClick={() => p.control("clear").then(() => toast("Queue cleared"))}>
            <Trash2 className="mr-1 h-3.5 w-3.5" /> Clear
          </Button>
        </div>
      </div>
      <div className="min-h-0 flex-1 space-y-3 overflow-auto px-2 py-3 scrollbar-thin">
        {!!history.length && (
          <section>
            <div className="mb-1 flex items-center gap-2 px-2 text-xs font-semibold uppercase tracking-wide text-subtle">
              <History className="h-3.5 w-3.5" /> History
            </div>
            {history.map((item, i) => row(item, i))}
          </section>
        )}
        <section>
          <div className="mb-1 px-2 text-xs font-semibold uppercase tracking-wide text-subtle">{upcoming.length ? "Now playing" : "Queue"}</div>
          {upcoming.map((item, i) => row(item, current + i))}
        </section>
        {!ids.length && <p className="px-2 text-sm text-muted">Queue is empty. Play a track from Home.</p>}
      </div>
      <div className="space-y-2 border-t border-border px-4 py-3">
        <label className="flex items-center justify-between text-sm">
          Autoplay
          <Switch checked={p.autoplay} onCheckedChange={p.setAutoplay} />
        </label>
        <label className="flex items-center justify-between text-sm">
          Stop after current
          <Switch checked={p.stopAfterCurrent} onCheckedChange={p.setStopAfterCurrent} />
        </label>
      </div>
      <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
        <DialogContent title="Save queue as playlist">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await p.saveQueueAsPlaylist(name);
              setSaveOpen(false);
            }}
          >
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} required />
            </Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setSaveOpen(false)}>Cancel</Button>
              <Button type="submit">Save</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
