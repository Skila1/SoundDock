import { useMemo, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Music } from "lucide-react";
import { api } from "@/lib/api";
import { TrackList } from "@/components/media/TrackList";
import { LibraryToolbar, LocalSearch, TrackGrid, useDebounced } from "@/components/media/LibraryToolbar";
import { Skeleton } from "@/components/ui/misc";
import { EmptyState, QueryError } from "@/components/ui/empty";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { usePlayer } from "@/stores/player";
import { useLibraryView } from "@/stores/libraryView";
import type { TrackPage, User } from "@/types/api";
import { toast } from "sonner";
import { clearCatalogueTracks, refreshCatalogue } from "@/lib/catalogue";

const SORTS = [
  { value: "recent", label: "Recently added" },
  { value: "oldest", label: "Oldest added" },
  { value: "title", label: "Title" },
  { value: "artist", label: "Artist" },
  { value: "album", label: "Album" },
  { value: "year", label: "Year" },
  { value: "duration", label: "Length" }
];

export function TracksPage() {
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const [view, patchView] = useLibraryView("catalogue-tracks", { sort: "recent" });
  const layout = view.layout;
  const [term, setTerm] = useState("");
  const search = useDebounced(term.trim(), 250);
  const sort = view.sort && view.sort !== "recent" ? view.sort : "";
  const q = useInfiniteQuery({
    queryKey: ["tracks", { q: search, sort }],
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams({ limit: "100" });
      if (pageParam) p.set("cursor", pageParam);
      if (search) p.set("q", search);
      if (sort) p.set("sort", sort);
      return api.get<TrackPage>(`/api/v1/tracks?${p}`);
    },
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
    placeholderData: (prev) => prev
  });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const tracks = useMemo(() => q.data?.pages.flatMap((p) => p.items || []) || [], [q.data]);
  const ids = tracks.map((t) => t.id);
  const [allOpen, setAllOpen] = useState(false);
  const [delFiles, setDelFiles] = useState(false);
  const admin = !!me.data?.is_admin;
  return (
    <div>
      <LibraryToolbar
        search={<LocalSearch value={term} onChange={setTerm} placeholder="Search the catalogue" />}
        sort={{ value: view.sort || "recent", options: SORTS, onChange: (v) => patchView({ sort: v }) }}
        filters={
          admin && tracks.length > 0 && !search ? (
            <Button variant="ghost" size="sm" className="text-destructive" onClick={() => { setDelFiles(false); setAllOpen(true); }}>
              Delete all
            </Button>
          ) : undefined
        }
        layout={layout}
        onLayout={(l) => patchView({ layout: l })}
        summary={search && !q.isFetching ? `${tracks.length}${q.hasNextPage ? "+" : ""} matching songs` : undefined}
      />
      {q.isError && <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />}
      {q.isLoading && (
        <div className="space-y-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      )}
      {!q.isLoading && !q.isError && !tracks.length && (
        <EmptyState icon={Music} title={search ? "No songs match." : "No tracks yet."} description={search ? "Try a different search." : undefined} />
      )}
      <div className={q.isPlaceholderData ? "opacity-60 transition-opacity" : "transition-opacity"}>
        {layout === "grid" ? (
          <TrackGrid tracks={tracks} onPlay={(i) => play([ids[i]])} />
        ) : (
          <TrackList
            tracks={tracks}
            onPlay={(i) => play([ids[i]])}
            onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
            onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
          />
        )}
      </div>
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
