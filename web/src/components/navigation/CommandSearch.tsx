import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { ListPlus, Search } from "lucide-react";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
import { Artwork } from "@/components/media/Artwork";
import { api } from "@/lib/api";
import { artworkUrl, cn } from "@/lib/utils";
import type { SearchHit } from "@/types/api";
import { useUi } from "@/stores/ui";
import { usePlayer } from "@/stores/player";

type RecentRow = { kind: "recent"; key: string; label: string };
type HitRow = { kind: "hit"; key: string; hit: SearchHit; source: "library" | "youtube" };
type CatalogRow = { kind: "catalog"; key: string; q: string };
type Row = RecentRow | HitRow | CatalogRow;

const LIBRARY_LIMIT = 2;
const YOUTUBE_LIMIT = 5;

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
  const nav = useNavigate();
  const add = usePlayer((s) => s.add);
  const inputRef = useRef<HTMLInputElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [library, setLibrary] = useState<SearchHit[]>([]);
  const [youtube, setYoutube] = useState<SearchHit[]>([]);
  const [libLoading, setLibLoading] = useState(false);
  const [ytLoading, setYtLoading] = useState(false);
  const [i, setI] = useState(0);
  const [panel, setPanel] = useState({ top: 0, left: 0, width: 480 });
  const recents = JSON.parse(localStorage.getItem("sd-recent-search") || "[]") as string[];

  const placePanel = () => {
    const el = wrapRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const width = Math.min(Math.max(r.width, 440), window.innerWidth - 16);
    let left = r.left + (r.width - width) / 2;
    if (left + width > window.innerWidth - 8) left = window.innerWidth - 8 - width;
    if (left < 8) left = 8;
    setPanel({ top: r.bottom + 8, left, width });
  };

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        inputRef.current?.focus();
        inputRef.current?.select();
        setOpen(true);
      } else if (e.key === "Escape" && (open || document.activeElement === inputRef.current)) {
        e.preventDefault();
        setOpen(false);
        inputRef.current?.blur();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  useEffect(() => {
    useUi.getState().set({ commandOpen: open });
  }, [open]);

  useLayoutEffect(() => {
    if (!open) return;
    placePanel();
    const onMove = () => placePanel();
    window.addEventListener("resize", onMove);
    window.addEventListener("scroll", onMove, true);
    return () => {
      window.removeEventListener("resize", onMove);
      window.removeEventListener("scroll", onMove, true);
    };
  }, [open, q]);

  useEffect(() => {
    if (!open) return;
    const onPtr = (e: PointerEvent) => {
      const t = e.target as Node;
      if (wrapRef.current?.contains(t) || panelRef.current?.contains(t)) return;
      setOpen(false);
    };
    window.addEventListener("pointerdown", onPtr);
    return () => window.removeEventListener("pointerdown", onPtr);
  }, [open]);

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
        .get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(term)}&type=track&limit=${LIBRARY_LIMIT}`)
        .then((r) => {
          if (!cancelled) setLibrary((r.results || []).filter((h) => h.type === "track").slice(0, LIBRARY_LIMIT));
        })
        .catch(() => {
          if (!cancelled) setLibrary([]);
        })
        .finally(() => {
          if (!cancelled) setLibLoading(false);
        });
      api
        .get<{ results: SearchHit[] }>(`/api/v1/search/youtube?q=${encodeURIComponent(term)}&limit=${YOUTUBE_LIMIT + 4}`)
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

  const remember = (term: string) => {
    if (!term.trim()) return;
    const next = [term, ...recents.filter((x) => x !== term)].slice(0, 8);
    localStorage.setItem("sd-recent-search", JSON.stringify(next));
  };

  const libraryHits = library.slice(0, LIBRARY_LIMIT);
  const ytHits = useMemo(
    () =>
      youtube
        .filter((h) => h.type === "youtube" && !sameSong(h.title, h.artist || "", libraryHits))
        .slice(0, YOUTUBE_LIMIT),
    [youtube, libraryHits]
  );
  const youtubeFirst = isYouTubeQuery(q);

  const queueHit = (h: SearchHit) => {
    remember(q);
    const hints = [{ id: h.id, title: h.title, artist: h.artist, duration_ms: h.duration_ms }];
    add([h.id], false, hints).then(() => toast.success(h.type === "youtube" ? "Downloading and adding to queue" : "Added to queue"));
  };

  const recentRows: RecentRow[] = !q.trim()
    ? recents.slice(0, 6).map((r) => ({ kind: "recent" as const, key: "recent-" + r, label: r }))
    : [];
  const libraryRows: HitRow[] = libraryHits.map((h) => ({
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
  }, [q, libraryHits.length, ytHits.length]);

  const activate = (row: Row) => {
    if (row.kind === "recent") {
      setQ(row.label);
      return;
    }
    if (row.kind === "hit") {
      queueHit(row.hit);
      return;
    }
    remember(row.q);
    setOpen(false);
    nav(`/search?q=${encodeURIComponent(row.q)}`);
  };

  const onKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      setOpen(false);
      inputRef.current?.blur();
      return;
    }
    if (!rows.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setOpen(true);
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
  const empty = searching && !libLoading && !ytLoading && libraryHits.length === 0 && ytHits.length === 0;
  const showPanel = open;

  const renderHit = (r: HitRow) => {
    const idx = rows.indexOf(r);
    const h = r.hit;
    return (
      <button
        key={r.key}
        type="button"
        className={cn(
          "flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left",
          idx === i ? "bg-surface-2" : "hover:bg-surface-2"
        )}
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => activate(r)}
        onMouseEnter={() => setI(idx)}
      >
        <div className="h-11 w-11 overflow-hidden rounded-md">
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
        <ListPlus className="h-4 w-4 shrink-0 text-muted" />
      </button>
    );
  };

  const dropdown = showPanel
    ? createPortal(
        <div
          ref={panelRef}
          style={{ top: panel.top, left: panel.left, width: panel.width }}
          className="fixed z-50 overflow-hidden rounded-xl border border-border bg-surface-1 shadow-card"
          id="header-search-results"
          onMouseDown={(e) => e.preventDefault()}
        >
          <div className="max-h-[min(28rem,calc(100vh-5.5rem))] overflow-auto p-2">
            {!searching && recentRows.length === 0 && (
              <p className="px-2 py-3 text-sm text-muted">Search your library, or paste a YouTube URL. Click a result to add it to the queue.</p>
            )}
            {recentRows.map((r) => {
              const idx = rows.indexOf(r);
              return (
                <button
                  key={r.key}
                  type="button"
                  className={cn(
                    "block w-full rounded-lg px-2 py-2 text-left text-sm text-muted hover:bg-surface-2",
                    idx === i && "bg-surface-2"
                  )}
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
                {(libLoading || libraryRows.length > 0) && <Section label="Library" loading={libLoading} />}
                {libraryRows.map(renderHit)}
                {(ytLoading || youtubeRows.length > 0) && <Section label="YouTube" loading={ytLoading} />}
                {youtubeRows.map(renderHit)}
              </>
            )}
            {empty && <p className="px-2 py-3 text-sm text-muted">No songs matched. Try another spelling or a YouTube URL.</p>}
            {catalogRow && (
              <button
                type="button"
                className={cn(
                  "mt-1 flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left text-sm",
                  rows.indexOf(catalogRow) === i ? "bg-surface-2" : "hover:bg-surface-2"
                )}
                onClick={() => activate(catalogRow)}
                onMouseEnter={() => setI(rows.indexOf(catalogRow))}
              >
                <Search className="h-4 w-4 shrink-0 text-muted" />
                <span>Search catalog for “{catalogRow.q}”</span>
              </button>
            )}
          </div>
        </div>,
        document.body
      )
    : null;

  return (
    <div ref={wrapRef} className="relative min-w-0 w-full">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
      <Input
        ref={inputRef}
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        onFocus={() => {
          setOpen(true);
          placePanel();
        }}
        onKeyDown={onKey}
        placeholder="Song, artist, or YouTube URL"
        className="h-10 rounded-full border-border bg-surface-2 pl-9 pr-14 focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
        role="combobox"
        aria-expanded={showPanel}
        aria-controls="header-search-results"
        autoComplete="off"
      />
      <kbd className="pointer-events-none absolute right-3 top-1/2 hidden -translate-y-1/2 rounded border border-border px-1.5 text-[10px] text-muted md:inline">
        ⌘K
      </kbd>
      {dropdown}
    </div>
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
