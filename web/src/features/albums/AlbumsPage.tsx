import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Disc3 } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { LibraryToolbar, LocalSearch, ProgressiveGrid } from "@/components/media/LibraryToolbar";
import { EmptyState, QueryError } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/misc";
import { useLibraryView } from "@/stores/libraryView";
import type { Album } from "@/types/api";

const SORTS = [
  { value: "default", label: "Default" },
  { value: "title", label: "Title" },
  { value: "artist", label: "Artist" },
  { value: "year", label: "Newest" }
];

const cmp = (a = "", b = "") => a.localeCompare(b, undefined, { sensitivity: "base", numeric: true });

export function AlbumsPage() {
  const q = useQuery({ queryKey: ["albums"], queryFn: () => api.get<Album[]>("/api/v1/albums") });
  const [view, patchView] = useLibraryView("catalogue-albums", { sort: "default" });
  const [term, setTerm] = useState("");
  const all = useMemo(() => q.data || [], [q.data]);
  const items = useMemo(() => {
    const words = term.trim().toLowerCase().split(/\s+/).filter(Boolean);
    const out = words.length ? all.filter((a) => words.every((w) => `${a.title} ${a.artist || ""} ${a.year || ""}`.toLowerCase().includes(w))) : [...all];
    if (view.sort === "title") out.sort((a, b) => cmp(a.title, b.title));
    else if (view.sort === "artist") out.sort((a, b) => cmp(a.artist, b.artist) || cmp(a.title, b.title));
    else if (view.sort === "year") out.sort((a, b) => (b.year || 0) - (a.year || 0));
    return out;
  }, [all, term, view.sort]);
  return (
    <div>
      {all.length > 0 && (
        <LibraryToolbar
          search={<LocalSearch value={term} onChange={setTerm} placeholder="Search albums" />}
          sort={{ value: view.sort || "default", options: SORTS, onChange: (v) => patchView({ sort: v }) }}
          summary={term ? `${items.length} of ${all.length} albums` : `${all.length} albums`}
        />
      )}
      {q.isError && <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />}
      {q.isLoading && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
          {Array.from({ length: 12 }).map((_, i) => <Skeleton key={i} className="aspect-square" />)}
        </div>
      )}
      {!q.isLoading && !q.isError && !all.length && <EmptyState icon={Disc3} title="No albums yet." description="Upload or scan a library to see albums here." />}
      {all.length > 0 && !items.length && <EmptyState icon={Disc3} title="No albums match." />}
      <ProgressiveGrid
        items={items}
        render={(a) => (
          <MediaCard key={a.id} className="w-full min-w-0 max-w-none" to={`/albums/${a.id}`} id={a.id} title={a.title} subtitle={a.artist || String(a.year || "")} kind="album" />
        )}
      />
    </div>
  );
}
