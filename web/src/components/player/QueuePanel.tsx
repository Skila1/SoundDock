import { History, PanelRightClose, Play, Trash2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Artwork } from "@/components/media/Artwork";
import { artworkUrl } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Track } from "@/types/api";
import { toast } from "sonner";
import { Switch } from "@/components/ui/switch";
import { useState } from "react";

export function QueuePanel({ onClose, onCollapse }: { onClose?: () => void; onCollapse?: () => void }) {
  const p = usePlayer();
  const [autoplay, setAutoplay] = useState(true);
  const ids = p.queue?.items?.map((i) => i.track_id) || [];
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

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex items-center gap-2">
          <h2 className="font-semibold">Queue</h2>
          <History className="h-4 w-4 text-muted" />
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
        <Button size="sm" variant="ghost" onClick={() => p.control("clear").then(() => toast("Queue cleared"))}>
          <Trash2 className="mr-1 h-3.5 w-3.5" /> Clear
        </Button>
      </div>
      <div className="min-h-0 flex-1 space-y-1 overflow-auto px-2 py-3 scrollbar-thin">
        {(p.queue?.items || []).map((item, i) => {
          const t = map.get(item.track_id);
          return (
            <div key={item.id} className={`flex items-center gap-3 rounded-md p-2 ${i === current ? "bg-surface-2" : "hover:bg-surface-2"}`}>
              <div className="h-10 w-10 overflow-hidden rounded">
                <Artwork src={artworkUrl("track", item.track_id, "thumb")} id={item.track_id} name={t?.title} kind="track" size="sm" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">{t?.title || "Track"}</div>
                <div className="truncate text-xs text-muted">
                  {t?.artists?.map((a) => a.name).join(", ") || t?.artist || (i === current ? "Now playing" : "Up next")}
                </div>
              </div>
              <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => p.playTracks(ids, i)} aria-label="Play now">
                <Play className="h-3.5 w-3.5" />
              </Button>
            </div>
          );
        })}
        {!ids.length && <p className="px-2 text-sm text-muted">Queue is empty. Play a track from Home.</p>}
      </div>
      <label className="flex items-center justify-between border-t border-border px-4 py-3 text-sm">
        Autoplay
        <Switch checked={autoplay} onCheckedChange={setAutoplay} />
      </label>
    </div>
  );
}
