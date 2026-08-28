import { useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Music } from "lucide-react";
import { api } from "@/lib/api";
import { TrackList } from "@/components/media/TrackList";
import { MediaCard } from "@/components/media/MediaCard";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState, QueryError } from "@/components/ui/empty";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import type { TrackPage, User } from "@/types/api";
import { toast } from "sonner";
import { clearCatalogueTracks, refreshCatalogue } from "@/lib/catalogue";

export function TracksPage() {
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const layout = useUi((s) => s.libraryLayout);
  const q = useInfiniteQuery({
    queryKey: ["tracks"],
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams({ limit: "100" });
      if (pageParam) p.set("cursor", pageParam);
      return api.get<TrackPage>(`/api/v1/tracks?${p}`);
    },
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined
  });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const tracks = q.data?.pages.flatMap((p) => p.items || []) || [];
  const ids = tracks.map((t) => t.id);
  const [allOpen, setAllOpen] = useState(false);
  const [delFiles, setDelFiles] = useState(false);
  const admin = !!me.data?.is_admin;
  return (
    <div>
      <div className="mb-4 flex flex-wrap justify-end gap-2">
            {admin && tracks.length > 0 && (
              <Button variant="destructive" size="sm" onClick={() => { setDelFiles(false); setAllOpen(true); }}>
                Delete all
              </Button>
            )}
            <LayoutToggle />
      </div>
      {q.isError && <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />}
      {!q.isLoading && !q.isError && !tracks.length && <EmptyState icon={Music} title="No tracks yet." />}
      {layout === "grid" ? (
        <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {tracks.map((t, i) => (
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
          tracks={tracks}
          onPlay={(i) => play([ids[i]])}
          onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
          onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
        />
      )}
      {q.hasNextPage && (
        <div className="mt-6 flex justify-center">
          <Button variant="secondary" disabled={q.isFetchingNextPage} onClick={() => q.fetchNextPage()}>
            {q.isFetchingNextPage ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}
      <Dialog open={allOpen} onOpenChange={setAllOpen}>
        <DialogContent title="Remove every track">
          <div className="space-y-3">
            <p className="text-sm text-muted">This clears the SoundDock catalogue. NAS, local, and external source files stay on disk.</p>
            <label className="flex items-center justify-between gap-3 text-sm">
              Also delete SoundDock-managed files
              <Switch checked={delFiles} onCheckedChange={setDelFiles} />
            </label>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setAllOpen(false)}>Cancel</Button>
              <Button
                type="button"
                variant="destructive"
                onClick={async () => {
                  try {
                    await api.post("/api/v1/tracks/bulk", { delete: true, all: true, delete_files: delFiles });
                    clearCatalogueTracks(qc);
                    toast.success("Removed all tracks");
                    setAllOpen(false);
                    refreshCatalogue(qc);
                  } catch {
                    toast.error("Could not remove tracks");
                  }
                }}
              >
                Remove all
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
