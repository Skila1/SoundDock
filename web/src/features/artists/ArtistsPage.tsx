import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Mic2 } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { LibraryToolbar, LocalSearch, ProgressiveGrid } from "@/components/media/LibraryToolbar";
import { EmptyState, QueryError } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/misc";

export function ArtistsPage() {
  const q = useQuery({ queryKey: ["artists"], queryFn: () => api.get<{ id: string; name: string }[]>("/api/v1/artists") });
  const [term, setTerm] = useState("");
  const all = useMemo(() => q.data || [], [q.data]);
  const items = useMemo(() => {
    const t = term.trim().toLowerCase();
    return t ? all.filter((a) => a.name.toLowerCase().includes(t)) : all;
  }, [all, term]);
  return (
    <div>
      {all.length > 0 && (
        <LibraryToolbar
          search={<LocalSearch value={term} onChange={setTerm} placeholder="Search artists" />}
          summary={term ? `${items.length} of ${all.length} artists` : `${all.length} artists`}
        />
      )}
      {q.isError && <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />}
      {q.isLoading && <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">{Array.from({ length: 12 }).map((_, i) => <Skeleton key={i} className="aspect-square rounded-full" />)}</div>}
      {!q.isLoading && !q.isError && !all.length && <EmptyState icon={Mic2} title="No artists yet." description="Scan a library or upload music to populate artists." />}
      {all.length > 0 && !items.length && <EmptyState icon={Mic2} title="No artists match." />}
      <ProgressiveGrid items={items} render={(a) => <MediaCard key={a.id} className="w-full min-w-0 max-w-none" to={`/artists/${a.id}`} id={a.id} title={a.name} kind="artist" />} />
    </div>
  );
}
