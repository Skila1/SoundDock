import { useEffect, useMemo, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent } from "react";
import { useNavigate } from "react-router-dom";
import { ListPlus, Search } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Artwork } from "@/components/media/Artwork";
import { api } from "@/lib/api";
import { artworkUrl } from "@/lib/utils";
import type { SearchHit } from "@/types/api";
import { useUi } from "@/stores/ui";
import { usePlayer } from "@/stores/player";

type RecentRow = { kind: "recent"; key: string; label: string };
type HitRow = { kind: "hit"; key: string; hit: SearchHit; source: "library" | "youtube" };
type CatalogRow = { kind: "catalog"; key: string; q: string };
type Row = RecentRow | HitRow | CatalogRow;

function isYouTubeQuery(q: string) {
  const t = q.trim();
  if (/^https?:\/\/(www\.|m\.|music\.)?(youtube\.com|youtu\.be)\//i.test(t)) return true;
  return /^[A-Za-z0-9_-]{11}$/.test(t);
}

function sameSong(title: string, artist: string, local: SearchHit[]) {
  const nt = title.trim().toLowerCase();
  const na = artist.trim().toLowerCase();
  if (!nt) return false;
  return local.some((row) => {
    if (row.type !== "track") return false;
    if ((row.title || "").trim().toLowerCase() !== nt) return false;
    if (!na) return true;
    const la = (row.artist || "").trim().toLowerCase();
    return la.includes(na) || na.includes(la);
  });
}

export function CommandSearch() {
  const ui = useUi();
  const nav = useNavigate();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const [q, setQ] = useState("");
  const [library, setLibrary] = useState<SearchHit[]>([]);
  const [youtube, setYoutube] = useState<SearchHit[]>([]);
  const [libLoading, setLibLoading] = useState(false);
  const [ytLoading, setYtLoading] = useState(false);
  const [i, setI] = useState(0);
  const recents = JSON.parse(localStorage.getItem("sd-recent-search") || "[]") as string[];

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        ui.set({ commandOpen: true });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [ui]);

  useEffect(() => {
    const term = q.trim();
    if (!term) {
      setLibrary([]);
      setYoutube([]);
      setLibLoading(false);
      setYtLoading(false);
      return;
    }
    let cancelled = false;
    setLibLoading(true);
    setYtLoading(true);
    const t = setTimeout(() => {
      api
        .get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(term)}&type=track&limit=8`)
        .then((r) => {
          if (!cancelled) setLibrary((r.results || []).filter((h) => h.type === "track"));
        })
        .catch(() => {
          if (!cancelled) setLibrary([]);
        })
        .finally(() => {
          if (!cancelled) setLibLoading(false);
        });
      api
        .get<{ results: SearchHit[] }>(`/api/v1/search/youtube?q=${encodeURIComponent(term)}&limit=8`)
        .then((r) => {
          if (!cancelled) setYoutube(r.results || []);
        })
        .catch(() => {
          if (!cancelled) setYoutube([]);
        })
        .finally(() => {
          if (!cancelled) setYtLoading(false);
        });
    }, 180);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [q]);

  const close = () => ui.set({ commandOpen: false });

  const remember = (term: string) => {
    if (!term.trim()) return;
    const next = [term, ...recents.filter((x) => x !== term)].slice(0, 8);
    localStorage.setItem("sd-recent-search", JSON.stringify(next));
  };

  const ytHits = useMemo(
    () => youtube.filter((h) => h.type === "youtube" && !sameSong(h.title, h.artist || "", library)),
    [youtube, library]
  );
  const youtubeFirst = isYouTubeQuery(q);

  const goHit = (h: SearchHit) => {
    remember(q);
    close();
    if (h.type === "track" || h.type === "youtube") play([h.id]);
    else nav(`/${h.type}s/${h.id}`);
  };

  const queueHit = (e: MouseEvent, h: SearchHit) => {
    e.preventDefault();
    e.stopPropagation();
    remember(q);
    add([h.id]).then(() => toast.success(h.type === "youtube" ? "Downloading and adding to queue" : "Added to queue"));
  };

  const recentRows: RecentRow[] = !q.trim()
    ? recents.map((r) => ({ kind: "recent" as const, key: "recent-" + r, label: r }))
    : [];
  const libraryRows: HitRow[] = library.map((h) => ({
    kind: "hit" as const,
    key: "library-" + h.id,
    hit: h,
    source: "library"
  }));
  const youtubeRows: HitRow[] = ytHits.map((h) => ({
    kind: "hit" as const,
    key: "youtube-" + h.id,
    hit: h,
    source: "youtube"
  }));
  const catalogRow: CatalogRow | null = q.trim()
    ? { kind: "catalog", key: "catalog", q: q.trim() }
    : null;
  const hitBlocks = youtubeFirst ? [...youtubeRows, ...libraryRows] : [...libraryRows, ...youtubeRows];
  const rows: Row[] = [...recentRows, ...hitBlocks, ...(catalogRow ? [catalogRow] : [])];

  useEffect(() => {
    setI(0);
  }, [q, library.length, ytHits.length]);

  useEffect(() => {
    if (ui.commandOpen) setI(0);
  }, [ui.commandOpen]);

  const activate = (row: Row) => {
    if (row.kind === "recent") setQ(row.label);
    else if (row.kind === "hit") goHit(row.hit);
    else {
      remember(row.q);
      close();
      nav(`/search?q=${encodeURIComponent(row.q)}`);
    }
  };

  const onKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (!rows.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setI((n) => (n + 1) % rows.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setI((n) => (n - 1 + rows.length) % rows.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const row = rows[i];
      if (row) activate(row);
    }
  };

  const searching = Boolean(q.trim());
  const empty = searching && !libLoading && !ytLoading && library.length === 0 && ytHits.length === 0;

  const renderHit = (r: HitRow) => {
    const idx = rows.indexOf(r);
    const h = r.hit;
    return (
      <div
        key={r.key}
        className={`flex w-full items-center gap-1 rounded-md px-1 py-1 ${idx === i ? "bg-surface-2" : "hover:bg-surface-2"}`}
        onMouseEnter={() => setI(idx)}
      >
        <button
          className="flex min-w-0 flex-1 items-center gap-3 rounded-md px-1 py-1 text-left"
          onClick={() => activate(r)}
        >
          <div className="h-9 w-9 overflow-hidden rounded">
            <Artwork
              src={h.type === "youtube" ? h.artwork_url || artworkUrl("youtube", h.id, "thumb") : artworkUrl(h.type, h.id, "thumb")}
              id={h.id}
              name={h.title}
              kind={h.type === "youtube" ? "track" : h.type}
              size="sm"
            />
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm" dangerouslySetInnerHTML={{ __html: highlight(h.title, q) }} />
            <div className="truncate text-xs text-muted">
              {r.source === "youtube" ? "YouTube" : "Library"}
              {h.artist ? ` · ${h.artist}` : h.album ? ` · ${h.album}` : ""}
            </div>
          </div>
        </button>
        <button
          type="button"
          className="rounded p-1 text-muted hover:bg-surface-3 hover:text-foreground"
          title="Add to queue"
          onClick={(e) => queueHit(e, h)}
        >
          <ListPlus className="h-4 w-4" />
        </button>
      </div>
    );
  };

  return (
    <Dialog open={ui.commandOpen} onOpenChange={(v) => ui.set({ commandOpen: v })}>
      <DialogContent className="p-0" title="Search songs">
        <Input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={onKey}
          placeholder="Song, artist, or YouTube URL"
          className="border-0 bg-transparent"
        />
        <div className="max-h-80 overflow-auto border-t border-border p-2">
          {!searching && recentRows.length === 0 && (
            <p className="px-2 py-3 text-sm text-muted">Search your library, or paste a YouTube URL. Matches from YouTube show as extras.</p>
          )}
          {recentRows.map((r) => {
            const idx = rows.indexOf(r);
            return (
              <button
                key={r.key}
                className={`block w-full rounded px-2 py-2 text-left text-sm text-muted hover:bg-surface-2 ${idx === i ? "bg-surface-2" : ""}`}
                onClick={() => activate(r)}
                onMouseEnter={() => setI(idx)}
              >
                {r.label}
              </button>
            );
          })}
          {searching && youtubeFirst && (
            <>
              <Section label="YouTube" loading={ytLoading} />
              {youtubeRows.map(renderHit)}
              {(libraryRows.length > 0 || libLoading) && <Section label="Library" loading={libLoading} />}
              {libraryRows.map(renderHit)}
            </>
          )}
          {searching && !youtubeFirst && (
            <>
              <Section label="Library" loading={libLoading} />
              {libraryRows.map(renderHit)}
              {(ytLoading || youtubeRows.length > 0) && <Section label="YouTube" loading={ytLoading} />}
              {youtubeRows.map(renderHit)}
            </>
          )}
          {empty && <p className="px-2 py-3 text-sm text-muted">No songs matched. Try another spelling or a YouTube URL.</p>}
          {catalogRow && (
            <button
              className={`mt-1 flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm ${rows.indexOf(catalogRow) === i ? "bg-surface-2" : "hover:bg-surface-2"}`}
              onClick={() => activate(catalogRow)}
              onMouseEnter={() => setI(rows.indexOf(catalogRow))}
            >
              <Search className="h-4 w-4 shrink-0 text-muted" />
              <span>Search catalog for “{catalogRow.q}”</span>
            </button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function Section({ label, loading }: { label: string; loading: boolean }) {
  return (
    <div className="flex items-center justify-between px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wide text-subtle">
      <span>{label}</span>
      {loading && <span className="font-normal normal-case tracking-normal text-muted">Searching…</span>}
    </div>
  );
}

function highlight(text: string, q: string) {
  if (!q) return text;
  const re = new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")})`, "ig");
  return text.replace(re, "<mark class='bg-accent/30 text-inherit'>$1</mark>");
}
