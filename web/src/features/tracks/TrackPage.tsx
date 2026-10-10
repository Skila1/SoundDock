import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { Download, Heart, ListPlus, Pencil, Pin, SkipForward } from "lucide-react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { CoverEditor } from "@/components/media/CoverEditor";
import { hasPerm } from "@/lib/perms";
import { HeroIconButton, MediaHero } from "@/components/media/MediaHero";
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
  if (!t) return <HeroSkeleton />;
  const fav = !!t.favourite || !!(favs.data || []).some((f) => f.type === "track" && f.id === t.id);
  const admin = !!me.data?.is_admin;
  const canEditCover = admin || hasPerm(me.data, "library.upload");
  const artist = t.artists?.map((a) => a.name).join(", ") || t.artist || "";
  const hires = (t.bit_depth || 0) >= 24 && (t.sample_rate || 0) >= 48000;

  const toggleFav = async () => {
    await api.post("/api/v1/favourites", { type: "track", id: t.id, on: !fav });
    qc.invalidateQueries({ predicate: (q) => q.queryKey[0] === "favourites" || q.queryKey[0] === "home" });
    qc.invalidateQueries({ queryKey: ["track-meta", id] });
    toast.success(fav ? "Removed from favourites" : "Favourited");
  };

  const specs = [
    t.codec,
    t.sample_rate ? `${t.sample_rate / 1000} kHz` : "",
    t.bit_depth ? `${t.bit_depth}-bit` : "",
    t.size_bytes ? formatBytes(t.size_bytes) : ""
  ].filter(Boolean);

  return (
    <div>
      <MediaHero
        art={
          <CoverEditor kind="track" id={t.id} canEdit={canEditCover} className="h-full w-full">
            <Artwork src={artworkUrl("track", t.id, "page")} id={t.id} name={t.title} kind="track" />
          </CoverEditor>
        }
        backdrop={artworkUrl("track", t.id, "thumb")}
        eyebrow="Song"
        title={t.title}
        meta={
          <>
            <span className="font-semibold">{artist}</span>
            {t.album_id ? (
              <>
                <span className="text-muted">·</span>
                <Link to={`/albums/${t.album_id}`} className="text-muted hover:text-foreground hover:underline">{t.album}</Link>
              </>
            ) : t.album ? <span className="text-muted">· {t.album}</span> : null}
            {t.year ? <span className="text-muted">· {t.year}</span> : null}
            <span className="text-muted">· {formatDuration(t.duration_ms)}</span>
          </>
        }
        stats={
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            {t.explicit && <Badge tone="warning">Explicit</Badge>}
            {hires && <Badge tone="accent">Hi-Res</Badge>}
            {t.genre && <Badge>{t.genre}</Badge>}
            {t.keep_forever && <Badge tone="success">Keep forever</Badge>}
            {t.media_unavailable && <Badge tone="warning">Unavailable - will reacquire</Badge>}
            {specs.length > 0 && <span className="text-xs text-subtle">{specs.join(" · ")}</span>}
            {t.play_count ? <span className="text-xs text-subtle">· {t.play_count} plays</span> : null}
          </div>
        }
        onPlay={() => play([t.id])}
        actions={
          <>
            <HeroIconButton label={fav ? "Remove from favourites" : "Favourite"} active={fav} onClick={toggleFav}><Heart className={fav ? "fill-current" : ""} /></HeroIconButton>
            <HeroIconButton label="Play next" onClick={() => add([t.id], true).then(() => toast.success("Playing next"))}><SkipForward /></HeroIconButton>
            <HeroIconButton label="Add to queue" onClick={() => add([t.id]).then(() => toast.success("Added to queue"))}><ListPlus /></HeroIconButton>
            <HeroIconButton label="Download" onClick={() => downloadTrack(t)}><Download /></HeroIconButton>
          </>
        }
        menu={[
          { label: "Add to playlist", icon: <ListPlus className="h-4 w-4" />, onSelect: () => useTrackActions.getState().openPlaylist([t.id]) },
          { label: "Edit song", icon: <Pencil className="h-4 w-4" />, onSelect: () => useTrackActions.getState().openEdit(t.id), hidden: !admin },
          {
            label: t.keep_forever ? "Allow pruning" : "Keep forever",
            icon: <Pin className="h-4 w-4" />,
            hidden: !admin,
            onSelect: async () => {
              await saveTrackMeta(t.id, { keep_forever: !t.keep_forever });
              qc.invalidateQueries({ queryKey: ["track-meta", id] });
              toast.success(t.keep_forever ? "Track can be pruned again" : "Marked Keep forever");
            }
          }
        ]}
      />
      {t.lyrics && (
        <section className="mb-8 max-w-2xl">
          <h2 className="mb-3 text-xl font-bold tracking-tight">Lyrics</h2>
          <div className="whitespace-pre-wrap text-[15px] leading-relaxed text-muted">{t.lyrics}</div>
        </section>
      )}
    </div>
  );
}

export function HeroSkeleton() {
  return (
    <div className="flex flex-col gap-6 pt-6 md:flex-row md:items-end">
      <div className="h-44 w-44 animate-pulse rounded-2xl bg-surface-2 md:h-56 md:w-56" />
      <div className="flex-1 space-y-3">
        <div className="h-3 w-20 animate-pulse rounded bg-surface-2" />
        <div className="h-10 w-2/3 animate-pulse rounded-lg bg-surface-2" />
        <div className="h-4 w-1/3 animate-pulse rounded bg-surface-2" />
      </div>
    </div>
  );
}
