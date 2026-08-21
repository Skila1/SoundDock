import { useQuery } from "@tanstack/react-query";
import { Disc3 } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { EmptyState } from "@/components/ui/empty";
import { PageHeader } from "@/components/ui/empty";
import type { Album } from "@/types/api";

export function AlbumsPage() {
  const q = useQuery({ queryKey: ["albums"], queryFn: () => api.get<Album[]>("/api/v1/albums") });
  return (
    <div>
      <PageHeader title="Albums" />
      {!q.isLoading && !q.data?.length && <EmptyState icon={Disc3} title="No albums yet." description="Upload or scan a library to see albums here." />}
      <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
        {(q.data || []).map((a) => (
          <MediaCard key={a.id} className="max-w-none min-w-0" to={`/albums/${a.id}`} id={a.id} title={a.title} subtitle={a.artist || String(a.year || "")} kind="album" />
        ))}
      </div>
    </div>
  );
}
