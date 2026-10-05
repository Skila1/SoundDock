import { useEffect, useRef, useState, type ReactNode } from "react";
import { ArrowDownAZ, ArrowUpAZ, Search, X } from "lucide-react";
import { Select } from "@/components/ui/select";
import { Tooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { LayoutToggle } from "./LayoutToggle";
import { TrackCard } from "./TrackActions";
import type { Track } from "@/types/api";

/** Search box that only ever filters the list it sits above. */
export function LocalSearch({
  value,
  onChange,
  placeholder = "Search",
  className
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  className?: string;
}) {
  const ref = useRef<HTMLInputElement>(null);
  return (
    <div className={cn("relative flex h-9 min-w-0 items-center", className)}>
      <Search className="pointer-events-none absolute left-2.5 h-4 w-4 text-subtle" />
      <input
        ref={ref}
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape" && value) {
            e.stopPropagation();
            onChange("");
          }
        }}
        placeholder={placeholder}
        aria-label={placeholder}
        className="h-9 w-full rounded-lg border border-border bg-surface-1 pl-8 pr-8 text-sm outline-none transition placeholder:text-subtle focus:border-accent/60 [&::-webkit-search-cancel-button]:hidden"
      />
      {value && (
        <button
          type="button"
          className="absolute right-1.5 rounded p-1 text-subtle hover:text-foreground"
          onClick={() => {
            onChange("");
            ref.current?.focus();
          }}
          aria-label="Clear search"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

/** Debounced copy of a value, for server-side search boxes. */
export function useDebounced<T>(value: T, ms = 250) {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setV(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return v;
}

export function LibraryToolbar({
  search,
  sort,
  desc,
  onDesc,
  filters,
  layout,
  onLayout,
  summary
}: {
  search: ReactNode;
  sort?: { value: string; options: { value: string; label: string }[]; onChange: (v: string) => void };
  desc?: boolean;
  onDesc?: (v: boolean) => void;
  filters?: ReactNode;
  layout?: "grid" | "list";
  onLayout?: (l: "grid" | "list") => void;
  summary?: ReactNode;
}) {
  return (
    <div className="mb-4 space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-[180px] flex-1 sm:max-w-sm">{search}</div>
        <div className="flex flex-wrap items-center gap-2 sm:ml-auto">
          {filters}
          {sort && (
            <div className="flex items-center gap-1">
              <Select className="h-9 w-[150px]" value={sort.value} onValueChange={sort.onChange} options={sort.options} />
              {onDesc && (
                <Tooltip label={desc ? "Descending" : "Ascending"}>
                  <button
                    type="button"
                    className="flex h-9 w-9 items-center justify-center rounded-lg bg-surface-2 text-muted hover:text-foreground"
                    onClick={() => onDesc(!desc)}
                    aria-label={desc ? "Sort descending" : "Sort ascending"}
                  >
                    {desc ? <ArrowUpAZ className="h-4 w-4" /> : <ArrowDownAZ className="h-4 w-4" />}
                  </button>
                </Tooltip>
              )}
            </div>
          )}
          {layout && <LayoutToggle value={layout} onChange={onLayout} />}
        </div>
      </div>
      {summary && <div className="text-xs text-subtle">{summary}</div>}
    </div>
  );
}

const PAGE = 120;

/**
 * Song grid that renders in pages as you scroll, so libraries with thousands of
 * songs stay responsive. Right-click works on every card.
 */
export function TrackGrid({ tracks, onPlay }: { tracks: Track[]; onPlay: (index: number) => void }) {
  const [shown, setShown] = useState(PAGE);
  const sentinel = useRef<HTMLDivElement>(null);
  // Start over only when the list itself changes (new search/sort), not on every render.
  const head = tracks[0]?.id;
  useEffect(() => setShown(PAGE), [head]);
  useEffect(() => {
    const el = sentinel.current;
    if (!el || shown >= tracks.length) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) setShown((n) => Math.min(tracks.length, n + PAGE));
      },
      { rootMargin: "800px 0px" }
    );
    io.observe(el);
    return () => io.disconnect();
  }, [shown, tracks.length]);
  return (
    <>
      <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
        {tracks.slice(0, shown).map((t, i) => (
          <TrackCard key={t.id} track={t} onPlay={() => onPlay(i)} />
        ))}
      </div>
      {shown < tracks.length && <div ref={sentinel} className="h-10" aria-hidden />}
    </>
  );
}
