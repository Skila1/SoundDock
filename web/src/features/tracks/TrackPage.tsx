import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { Download, Heart, ListPlus, Pencil, Play, SkipForward } from "lucide-react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { CoverEditor } from "@/components/media/CoverEditor";
import { hasPerm } from "@/lib/perms";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { artworkUrl, formatDuration, formatBytes } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Favourite, Track, User } from "@/types/api";
import { toast } from "sonner";
import { downloadTrack, saveTrackMeta } from "@/components/media/TrackList";
import { useTrackActions } from "@/components/media/TrackActions";

export type TrackMeta = Track & {
  genre?: string;
  isrc?: string;
  mbid?: string;
  locked?: boolean;
  lyrics?: string;
  codec?: string;
  container?: string;
  bit_depth?: number | null;
  sample_rate?: number | null;
  bitrate?: number | null;
  channels?: number | null;
  size_bytes?: number | null;
  play_count?: number;
  last_played_at?: string | null;
  favourite?: boolean;
  organisation_mode?: string;
  read_only?: boolean;
  write_back_supported?: boolean;
  metadata_source?: string;
  keep_forever?: boolean;
  media_unavailable?: boolean;
  acquisition?: string;
};

async function loadTrack(id: string): Promise<TrackMeta> {
  try {
    return await api.get<TrackMeta>(`/api/v1/tracks/${id}/metadata`);
  } catch {
    return await api.get<TrackMeta>(`/api/v1/tracks/${id}`);
  }
}

export function TrackPage() {
  const { id } = useParams();
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const q = useQuery({ queryKey: ["track-meta", id], queryFn: () => loadTrack(id!), enabled: !!id });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const t = q.data;
  if (!t) return <div className="h-64 animate-pulse rounded-xl bg-surface-2" />;
  const fav = !!t.favourite || !!(favs.data || []).some((f) => f.type === "track" && f.id === t.id);
  const admin = !!me.data?.is_admin;
  const canEditCover = admin || hasPerm(me.data, "library.upload");
  const artist = t.artists?.map((a) => a.name).join(", ") || t.artist || "";
  const hires = (t.bit_depth || 0) >= 24 && (t.sample_rate || 0) >= 48000;

  const toggleFav = async () => {
    await api.post("/api/v1/favourites", { type: "track", id: t.id, on: !fav });
    qc.invalidateQueries({ queryKey: ["favourites"] });
    qc.invalidateQueries({ queryKey: ["track-meta", id] });
    toast.success(fav ? "Removed from favourites" : "Favourited");
  };

  return (
    <div>
      <div className="mb-8 flex flex-col gap-6 md:flex-row">
        <CoverEditor kind="track" id={t.id} canEdit={canEditCover} className="h-52 w-52 overflow-hidden rounded-xl shadow-card">
          <Artwork src={artworkUrl("track", t.id, "page")} id={t.id} name={t.title} kind="track" />
        </CoverEditor>
        <div className="flex flex-col justify-end">
          <p className="text-xs uppercase tracking-widest text-subtle">Track</p>
          <h1 className="text-4xl font-semibold md:text-5xl">{t.title}</h1>
          <p className="mt-2 text-muted">
            {artist}
            {t.album_id ? (
              <>
                {" · "}
                <Link to={`/albums/${t.album_id}`} className="hover:underline">{t.album}</Link>
              </>
            ) : t.album ? ` · ${t.album}` : ""}
            {t.year ? ` · ${t.year}` : ""}
          </p>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {t.explicit && <Badge tone="warning">Explicit</Badge>}
            {t.media_unavailable && <Badge tone="warning">Unavailable - will reacquire</Badge>}
            {t.keep_forever && <Badge tone="success">Keep forever</Badge>}
            {t.codec && <Badge>{t.codec}</Badge>}
            {hires && <Badge tone="accent">Hi-Res</Badge>}
            {t.genre && <Badge>{t.genre}</Badge>}
          </div>
          <p className="mt-2 text-sm text-subtle">
            {formatDuration(t.duration_ms)}
            {t.sample_rate ? ` · ${t.sample_rate / 1000} kHz` : ""}
            {t.bit_depth ? ` · ${t.bit_depth}-bit` : ""}
            {t.size_bytes ? ` · ${formatBytes(t.size_bytes)}` : ""}
            {t.play_count ? ` · ${t.play_count} plays` : ""}
          </p>
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={() => play([t.id])}><Play className="fill-current" /> Play</Button>
            <Button variant="secondary" onClick={() => add([t.id], true).then(() => toast.success("Playing next"))}><SkipForward /> Play next</Button>
            <Button variant="ghost" onClick={() => add([t.id]).then(() => toast.success("Added to queue"))}><ListPlus /> Add to queue</Button>
            <Button variant="ghost" onClick={toggleFav} aria-label="Favourite"><Heart className={fav ? "fill-current" : ""} /></Button>
            <Button variant="ghost" onClick={() => downloadTrack(t)}><Download /> Download</Button>
            {admin && <Button variant="ghost" onClick={() => useTrackActions.getState().openEdit(t.id)}><Pencil /> Edit</Button>}
            {admin && (
              <Button
                variant="ghost"
                onClick={async () => {
                  await saveTrackMeta(t.id, { keep_forever: !t.keep_forever });
                  qc.invalidateQueries({ queryKey: ["track-meta", id] });
                  toast.success(t.keep_forever ? "Track can be pruned again" : "Marked Keep forever");
                }}
              >
                {t.keep_forever ? "Allow prune" : "Keep forever"}
              </Button>
            )}
          </div>
        </div>
      </div>
      {t.lyrics && (
        <section className="mb-8 max-w-xl whitespace-pre-wrap text-sm text-muted">{t.lyrics}</section>
      )}
    </div>
  );
}
