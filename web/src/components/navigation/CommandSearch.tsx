import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Artwork } from "@/components/media/Artwork";
import { api } from "@/lib/api";
import { artworkUrl } from "@/lib/utils";
import type { SearchHit } from "@/types/api";
import { useUi } from "@/stores/ui";
import { usePlayer } from "@/stores/player";

export function CommandSearch() {
  const ui = useUi();
  const nav = useNavigate();
  const play = usePlayer((s) => s.playTracks);
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [i, setI] = useState(0);
  const recents = JSON.parse(localStorage.getItem("sd-recent-search") || "[]") as string[];

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        ui.set({ commandOpen: true });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [ui]);

  useEffect(() => {
    if (!q.trim()) {
      setHits([]);
      return;
    }
    const t = setTimeout(() => {
      api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&limit=12`).then((r) => {
        setHits(r.results || []);
        setI(0);
      });
    }, 180);
    return () => clearTimeout(t);
  }, [q]);

  const go = (h: SearchHit) => {
    const next = [q, ...recents.filter((x) => x !== q)].slice(0, 8);
    localStorage.setItem("sd-recent-search", JSON.stringify(next));
    ui.set({ commandOpen: false });
    if (h.type === "track") play([h.id]);
    else nav(`/${h.type}s/${h.id}`);
  };

  return (
    <Dialog open={ui.commandOpen} onOpenChange={(v) => ui.set({ commandOpen: v })}>
      <DialogContent className="p-0" title="Search">
        <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search tracks, albums, artists…" className="border-0 bg-transparent" />
        <div className="max-h-80 overflow-auto border-t border-border p-2">
          {!q && recents.map((r) => (
            <button key={r} className="block w-full rounded px-2 py-2 text-left text-sm text-muted hover:bg-surface-2" onClick={() => setQ(r)}>
              {r}
            </button>
          ))}
          {hits.map((h, idx) => (
            <button
              key={h.type + h.id}
              className={`flex w-full items-center gap-3 rounded-md px-2 py-2 text-left ${idx === i ? "bg-surface-2" : "hover:bg-surface-2"}`}
              onClick={() => go(h)}
              onMouseEnter={() => setI(idx)}
            >
              <div className="h-9 w-9 overflow-hidden rounded">
                <Artwork src={artworkUrl(h.type === "track" ? "track" : (h.type as any), h.id, "thumb")} id={h.id} name={h.title} kind={h.type as any} size="sm" />
              </div>
              <div className="min-w-0">
                <div className="truncate text-sm" dangerouslySetInnerHTML={{ __html: highlight(h.title, q) }} />
                <div className="text-xs text-muted">{h.type} · {h.artist || h.album || ""}</div>
              </div>
            </button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function highlight(text: string, q: string) {
  if (!q) return text;
  const re = new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")})`, "ig");
  return text.replace(re, "<mark class='bg-accent/30 text-inherit'>$1</mark>");
}
