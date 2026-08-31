import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { UserRound } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import type { PublicUserProfile } from "@/types/api";

export function PublicProfilePage() {
  const { id } = useParams();
  const q = useQuery({
    queryKey: ["user-profile", id],
    enabled: !!id,
    queryFn: () => api.get<PublicUserProfile>(`/api/v1/users/${id}`)
  });
  const p = q.data;

  return (
    <div className="max-w-2xl">
      <PageHeader
        title={p?.display_name || p?.username || "Listener"}
        description="Public SoundDock profile. Personal libraries stay private unless the owner opens them."
        actions={
          p ? (
            <Badge tone={p.personal_library_visibility === "public" ? "success" : "neutral"}>
              Library {p.personal_library_visibility === "public" ? "public" : "private"}
            </Badge>
          ) : null
        }
      />
      {q.isError && (
        <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />
      )}
      {p && !p.personal_library_visible && (
        <EmptyState
          icon={UserRound}
          title="This personal library is private."
          description="Only the owner, or an administrator using user management, can open it."
        />
      )}
      {p?.personal_library_visible && (
        <div className="rounded-xl border border-border bg-surface-1 p-5">
          <p className="text-sm text-muted">
            {p.personal_library_track_count === 1
              ? "1 requested song"
              : `${p.personal_library_track_count} requested songs`}
          </p>
          <Button asChild className="mt-4" size="sm">
            <Link to={`/users/${p.id}/library`}>Open personal library</Link>
          </Button>
        </div>
      )}
    </div>
  );
}
