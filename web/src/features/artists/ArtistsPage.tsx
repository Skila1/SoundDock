import { useQuery } from "@tanstack/react-query";
import { Mic2 } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { EmptyState } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/misc";

export function ArtistsPage() {
  const q = useQuery({ queryKey: ["artists"], queryFn: () => api.get<{ id: string; name: string }[]>("/api/v1/artists") });
  return (
    <div>
      {q.isLoading && <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">{Array.from({ length: 12 }).map((_, i) => <Skeleton key={i} className="aspect-square rounded-full" />)}</div>}
      {!q.isLoading && !q.data?.length && <EmptyState icon={Mic2} title="No artists yet." description="Scan a library or upload music to populate artists." />}
      <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
        {(q.data || []).map((a) => <MediaCard key={a.id} className="max-w-none min-w-0" to={`/artists/${a.id}`} id={a.id} title={a.name} kind="artist" />)}
      </div>
    </div>
  );
}
