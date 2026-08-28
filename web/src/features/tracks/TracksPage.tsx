import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Music } from "lucide-react";
import { api } from "@/lib/api";
import { TrackList } from "@/components/media/TrackList";
import { MediaCard } from "@/components/media/MediaCard";
import { LayoutToggle } from "@/components/media/LayoutToggle";
import { EmptyState } from "@/components/ui/empty";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import type { Track, User } from "@/types/api";
import { toast } from "sonner";

export function TracksPage() {
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const layout = useUi((s) => s.libraryLayout);
  const q = useQuery({ queryKey: ["tracks"], queryFn: () => api.get<Track[]>("/api/v1/tracks") });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const tracks = q.data || [];
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
      {!q.isLoading && !tracks.length && <EmptyState icon={Music} title="No tracks yet." />}
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
                  await api.post("/api/v1/tracks/bulk", { delete: true, all: true, delete_files: delFiles });
                  toast.success("Delete queued");
                  setAllOpen(false);
                  qc.invalidateQueries({ queryKey: ["tracks"] });
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
