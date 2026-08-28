import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { Download, Heart, ListPlus, Pencil, Play, SkipForward } from "lucide-react";
import { useRef, useState } from "react";
import { api } from "@/lib/api";
import { Artwork } from "@/components/media/Artwork";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/misc";
import { artworkUrl, formatDuration, formatBytes } from "@/lib/utils";
import { usePlayer } from "@/stores/player";
import type { Favourite, Track, User } from "@/types/api";
import { toast } from "sonner";
import { callWriteBack, downloadTrack, saveTrackMeta, uploadArtwork } from "@/components/media/TrackList";

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
  const fileRef = useRef<HTMLInputElement>(null);
  const q = useQuery({ queryKey: ["track-meta", id], queryFn: () => loadTrack(id!), enabled: !!id });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const [edit, setEdit] = useState(false);
  const t = q.data;
  if (!t) return <div className="h-64 animate-pulse rounded-xl bg-surface-2" />;
  const fav = !!t.favourite || !!(favs.data || []).some((f) => f.type === "track" && f.id === t.id);
  const admin = !!me.data?.is_admin;
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
        <button type="button" className="h-52 w-52 overflow-hidden rounded-xl shadow-card" onClick={() => admin && fileRef.current?.click()} aria-label="Track artwork">
          <Artwork src={artworkUrl("track", t.id, "page")} id={t.id} name={t.title} kind="track" />
        </button>
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={async (e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (!file || !id) return;
            try {
              await uploadArtwork("track", id, file);
              toast.success("Artwork saved");
              qc.invalidateQueries({ queryKey: ["track-meta", id] });
            } catch {
              toast.error("Artwork upload is not available yet");
            }
          }}
        />
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
            {t.media_unavailable && <Badge tone="warning">Unavailable — will reacquire</Badge>}
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
            {admin && <Button variant="ghost" onClick={() => setEdit(true)}><Pencil /> Edit</Button>}
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
      {edit && <TrackEditDialog track={t} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); qc.invalidateQueries({ queryKey: ["track-meta", id] }); }} />}
    </div>
  );
}

function TrackEditDialog({ track, onClose, onSaved }: { track: TrackMeta; onClose: () => void; onSaved: () => void }) {
  const [title, setTitle] = useState(track.title);
  const [artist, setArtist] = useState(track.artists?.map((a) => a.name).join(", ") || track.artist || "");
  const [genre, setGenre] = useState(track.genre || "");
  const [year, setYear] = useState(track.year ? String(track.year) : "");
  const [disc, setDisc] = useState(String(track.disc_number || 1));
  const [num, setNum] = useState(String(track.track_number || 0));
  const [isrc, setIsrc] = useState(track.isrc || "");
  const [lyrics, setLyrics] = useState(track.lyrics || "");
  const [explicit, setExplicit] = useState(!!track.explicit);
  const [writeBack, setWriteBack] = useState(false);

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent title="Edit metadata" className="max-h-[90vh] overflow-auto">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const y = year.trim() ? Number(year) : undefined;
            await saveTrackMeta(track.id, {
              title,
              artist,
              genre,
              year: y && !Number.isNaN(y) ? y : undefined,
              disc_number: Number(disc) || 1,
              track_number: Number(num) || 0,
              isrc,
              lyrics,
              explicit,
              write_back: writeBack
            });
            toast.success("Metadata saved");
            if (writeBack) await callWriteBack([track.id], true);
            onSaved();
          }}
        >
          <Field label="Title"><Input value={title} onChange={(e) => setTitle(e.target.value)} /></Field>
          <Field label="Artists"><Input value={artist} onChange={(e) => setArtist(e.target.value)} /></Field>
          <Field label="Genre"><Input value={genre} onChange={(e) => setGenre(e.target.value)} /></Field>
          <div className="grid grid-cols-3 gap-2">
            <Field label="Year"><Input value={year} onChange={(e) => setYear(e.target.value)} inputMode="numeric" /></Field>
            <Field label="Disc"><Input value={disc} onChange={(e) => setDisc(e.target.value)} inputMode="numeric" /></Field>
            <Field label="Track"><Input value={num} onChange={(e) => setNum(e.target.value)} inputMode="numeric" /></Field>
          </div>
          <Field label="ISRC"><Input value={isrc} onChange={(e) => setIsrc(e.target.value)} /></Field>
          <Field label="Lyrics"><Textarea value={lyrics} onChange={(e) => setLyrics(e.target.value)} /></Field>
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm">Explicit</span>
            <Switch checked={explicit} onCheckedChange={setExplicit} />
          </div>
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm">Write tags to file</span>
            <Switch checked={writeBack} onCheckedChange={setWriteBack} />
          </div>
          <p className="text-xs text-subtle">
            {track.write_back_supported ? "Managed library — P3 write-back when registered." : "DB save always. File write-back is managed libraries only (P3)."}
          </p>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
