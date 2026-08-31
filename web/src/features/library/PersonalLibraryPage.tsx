import { useMemo } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Library } from "lucide-react";
import { api } from "@/lib/api";
import { TrackList } from "@/components/media/TrackList";
import { MediaCard } from "@/components/media/MediaCard";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { Button } from "@/components/ui/button";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { Badge } from "@/components/ui/misc";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import type { PersonalLibraryResponse, PublicUserProfile, User } from "@/types/api";
import { toast } from "sonner";

export function PersonalLibraryPage({ mine, admin, adminDiscord }: { mine?: boolean; admin?: boolean; adminDiscord?: boolean }) {
  const { id, discordID } = useParams();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const layout = useUi((s) => s.libraryLayout);
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const path = mine
    ? "/api/v1/me/library?limit=200"
    : adminDiscord && discordID
      ? `/api/v1/admin/discord-users/${encodeURIComponent(discordID)}/library?limit=200`
    : admin && id
      ? `/api/v1/admin/users/${id}/library?limit=200`
      : `/api/v1/users/${id}/library?limit=200`;
  const profile = useQuery({
    queryKey: ["user-profile", id],
    enabled: !mine && !adminDiscord && !!id,
    queryFn: () => api.get<PublicUserProfile>(`/api/v1/users/${id}`)
  });
  const q = useQuery({
    queryKey: ["personal-library", mine ? "me" : adminDiscord ? discordID : id, admin || adminDiscord ? "admin" : "user"],
    queryFn: () => api.get<PersonalLibraryResponse>(path)
  });
  const items = q.data?.items || [];
  const ids = useMemo(() => items.map((t) => t.id), [items]);
  const title = mine
    ? "My Library"
    : q.data?.owner.display_name || profile.data?.display_name || "Personal library";
  const visibility = q.data?.owner.visibility || profile.data?.personal_library_visibility || "private";

  return (
    <div>
      <PageHeader
        title={title}
        description={
          mine
            ? "Songs you have requested in SoundDock or through Discord. The shared catalogue stays under Catalogue."
            : "Requested songs for this listener."
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={visibility === "public" ? "success" : "neutral"}>{visibility === "public" ? "Public" : "Private"}</Badge>
            {mine && (
              <Button asChild size="sm" variant="secondary">
                <Link to="/profile">Visibility</Link>
              </Button>
            )}
            {items.length > 0 && (
              <Button size="sm" onClick={() => play(ids)}>Play all</Button>
            )}
            {items.length > 0 && (
              <Button size="sm" variant="secondary" onClick={() => add(ids).then(() => toast.success("Queued library"))}>
                Queue all
              </Button>
            )}
            <LayoutToggle />
          </div>
        }
      />
      {q.data?.inspecting && (
        <p className="mb-4 rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm text-muted">
          You are viewing this personal library as an administrator. It is not mixed into your own library.
        </p>
      )}
      {q.isError && profile.data && !profile.data.personal_library_visible && (
        <EmptyState
          icon={Library}
          title="This personal library is private."
          description="Only the owner, or an administrator from user management, can open it."
        />
      )}
      {q.isError && !(profile.data && !profile.data.personal_library_visible) && (
        <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />
      )}
      {!q.isLoading && !q.isError && !items.length && (
        <EmptyState
          icon={Library}
          title={mine ? "Nothing requested yet." : "This library is empty."}
          description={mine ? "Play or queue a song from Search or the catalogue and it will land here." : undefined}
          action={mine ? { label: "Search", onClick: () => (location.href = "/search") } : undefined}
        />
      )}
      {layout === "grid" ? (
        <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {items.map((t, i) => (
            <MediaCard
              key={t.id}
              className="max-w-none min-w-0 w-full"
              to={`/tracks/${t.id}`}
              id={t.id}
              title={t.title}
              subtitle={t.artist || t.album}
              kind="track"
              explicit={t.explicit}
              onPlay={() => play([ids[i]])}
            />
          ))}
        </div>
      ) : (
        <TrackList
          tracks={items}
          onPlay={(i) => play([ids[i]])}
          onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
          onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
        />
      )}
      {mine && me.data && (
        <p className="mt-6 text-xs text-subtle">
          The shared catalogue is still at <Link className="underline" to="/library">Catalogue</Link>.
          Open a track&apos;s menu to add it to one of your playlists.
        </p>
      )}
    </div>
  );
}
