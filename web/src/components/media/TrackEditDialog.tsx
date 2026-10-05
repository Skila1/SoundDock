import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ImagePlus, Loader2, Lock, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { artworkUrl } from "@/lib/utils";
import { refreshCatalogue } from "@/lib/catalogue";
import { useArtworkVersion } from "@/stores/artwork";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/misc";
import { Artwork } from "./Artwork";
import type { Track } from "@/types/api";

export type TrackMetaFull = Track & {
  genre?: string;
  isrc?: string | null;
  mbid?: string | null;
  locked?: boolean;
  lyrics?: string;
  lyrics_timed?: boolean;
  codec?: string | null;
  container?: string | null;
  bit_depth?: number | null;
  sample_rate?: number | null;
  bitrate?: number | null;
  size_bytes?: number | null;
  manual_gain_db?: number | null;
  keep_forever?: boolean;
  write_back_supported?: boolean;
  read_only?: boolean;
  locks?: string[];
  metadata_source?: string;
};

type Form = {
  title: string;
  artist: string;
  album: string;
  genre: string;
  year: string;
  disc: string;
  track: string;
  isrc: string;
  mbid: string;
  gain: string;
  lyrics: string;
  explicit: boolean;
  keepForever: boolean;
  locked: boolean;
};

const MAX_ART = 20 << 20;

function toForm(t: TrackMetaFull): Form {
  return {
    title: t.title || "",
    artist: t.artists?.map((a) => a.name).join(", ") || t.artist || "",
    album: t.album || "",
    genre: t.genre || "",
    year: t.year ? String(t.year) : "",
    disc: String(t.disc_number || 1),
    track: t.track_number ? String(t.track_number) : "",
    isrc: t.isrc || "",
    mbid: t.mbid || "",
    gain: t.manual_gain_db != null ? String(t.manual_gain_db) : "",
    lyrics: t.lyrics || "",
    explicit: !!t.explicit,
    keepForever: !!t.keep_forever,
    locked: !!t.locked
  };
}

const int = (v: string) => {
  const n = Number(v.trim());
  return v.trim() && Number.isFinite(n) ? Math.trunc(n) : undefined;
};

/** Full metadata + artwork editor for one catalogue track (admins). */
export function TrackEditDialog({ trackId, onClose }: { trackId: string; onClose: () => void }) {
  const qc = useQueryClient();
  const meta = useQuery({
    queryKey: ["track-meta", trackId],
    queryFn: () => api.get<TrackMetaFull>(`/api/v1/tracks/${trackId}/metadata`),
    staleTime: 0
  });
  const [form, setForm] = useState<Form | null>(null);
  const [art, setArt] = useState<File | null>(null);
  const [artPreview, setArtPreview] = useState<string | null>(null);
  const [resetArt, setResetArt] = useState(false);
  const [albumArt, setAlbumArt] = useState(false);
  const [writeBack, setWriteBack] = useState(false);
  const [saving, setSaving] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const version = useArtworkVersion((s) => s.version);

  useEffect(() => {
    if (meta.data && !form) setForm(toForm(meta.data));
  }, [meta.data, form]);

  useEffect(() => {
    if (!art) {
      setArtPreview(null);
      return;
    }
    const url = URL.createObjectURL(art);
    setArtPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [art]);

  const initial = useMemo(() => (meta.data ? toForm(meta.data) : null), [meta.data]);
  const t = meta.data;
  const fieldLocks = new Set(t?.locks || []);
  const lockedNow = !!form?.locked;

  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => (f ? { ...f, [k]: v } : f));

  const pickArt = (file: File | undefined) => {
    if (!file) return;
    if (!file.type.startsWith("image/")) {
      toast.error("Choose an image file (JPEG, PNG, WebP or GIF).");
      return;
    }
    if (file.size > MAX_ART) {
      toast.error("That image is larger than 20 MB.");
      return;
    }
    setResetArt(false);
    setArt(file);
  };

  const save = async () => {
    if (!form || !initial || !t) return;
    const body: Record<string, unknown> = {};
    const changed = (k: keyof Form) => form[k] !== initial[k];
    if (changed("locked")) body.locked = form.locked;
    if (!form.locked) {
      if (changed("title") && form.title.trim()) body.title = form.title.trim();
      if (changed("artist")) body.artist = form.artist;
      if (changed("album")) body.album = form.album.trim();
      if (changed("genre")) body.genre = form.genre.trim();
      if (changed("year")) {
        const y = int(form.year);
        if (y != null) body.year = y;
      }
      if (changed("disc")) body.disc_number = int(form.disc) ?? 1;
      if (changed("track")) body.track_number = int(form.track) ?? 0;
      if (changed("isrc")) body.isrc = form.isrc.trim();
      if (changed("mbid")) body.mbid = form.mbid.trim();
      if (changed("gain")) {
        const g = Number(form.gain.trim() || "0");
        if (Number.isFinite(g)) body.manual_gain_db = g;
      }
      if (changed("lyrics")) body.lyrics = form.lyrics;
      if (changed("explicit")) body.explicit = form.explicit;
    }
    if (changed("keepForever")) body.keep_forever = form.keepForever;
    if (writeBack) body.write_back = true;
    setSaving(true);
    try {
      if (Object.keys(body).length) await api.patch(`/api/v1/tracks/${t.id}/metadata`, body);
      if (art) {
        const fd = new FormData();
        fd.append("file", art);
        await api.post(`/api/v1/tracks/${t.id}/artwork`, fd);
        if (albumArt && t.album_id) {
          const afd = new FormData();
          afd.append("file", art);
          await api.post(`/api/v1/albums/${t.album_id}/artwork`, afd);
        }
      } else if (resetArt) {
        await api.del(`/api/v1/tracks/${t.id}/artwork`);
      }
      if (writeBack) {
        await api
          .post(`/api/v1/tracks/${t.id}/writeback`, { write_tags: true, write_artwork: !!art, managed_only: true })
          .catch(() => toast.message("Saved. File write-back is not available for this library."));
      }
      if (art || resetArt) useArtworkVersion.getState().bump();
      refreshCatalogue(qc);
      for (const key of ["personal-library", "track", "queue", "me-queue"]) void qc.invalidateQueries({ queryKey: [key] });
      toast.success("Song updated");
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save changes");
    } finally {
      setSaving(false);
    }
  };

  const lockHint = (field: string) => (fieldLocks.has(field) ? "Locked field - changes are ignored" : undefined);
  const coverSrc = artPreview || (resetArt ? undefined : t ? artworkUrl("track", t.id, "page") : undefined);

  return (
    <Dialog open onOpenChange={(v) => !v && !saving && onClose()}>
      <DialogContent title="Edit song" className="max-h-[92vh] w-[min(760px,calc(100vw-2rem))] overflow-y-auto scrollbar-thin">
        {!form || !t ? (
          meta.isError ? (
            <p className="text-sm text-destructive">Could not load this song.</p>
          ) : (
            <div className="space-y-3">
              <Skeleton className="h-40 w-40" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          )
        ) : (
          <form
            className="space-y-5"
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            <div className="grid gap-5 sm:grid-cols-[176px_minmax(0,1fr)]">
              <div className="space-y-2">
                <button
                  type="button"
                  className="group relative block aspect-square w-full overflow-hidden rounded-lg bg-surface-2 shadow-card"
                  onClick={() => fileRef.current?.click()}
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={(e) => {
                    e.preventDefault();
                    pickArt(e.dataTransfer.files?.[0]);
                  }}
                  aria-label="Replace artwork"
                >
                  {artPreview ? (
                    <img src={artPreview} alt="" className="h-full w-full object-cover" />
                  ) : (
                    <Artwork key={`${version}-${resetArt}`} src={coverSrc} id={t.id} name={t.title} kind="track" />
                  )}
                  <span className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-black/55 text-xs font-medium text-white opacity-0 transition group-hover:opacity-100">
                    <ImagePlus className="h-5 w-5" />
                    Replace artwork
                  </span>
                </button>
                <input ref={fileRef} type="file" accept="image/*" className="hidden" onChange={(e) => pickArt(e.target.files?.[0] || undefined)} />
                <div className="flex gap-1">
                  <Button type="button" size="sm" variant="secondary" className="flex-1" onClick={() => fileRef.current?.click()}>
                    Upload
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    title="Use the embedded artwork"
                    onClick={() => {
                      setArt(null);
                      setResetArt(true);
                    }}
                  >
                    <RotateCcw className="h-3.5 w-3.5" />
                  </Button>
                </div>
                {art && t.album_id && (
                  <label className="flex items-center gap-2 text-xs text-muted">
                    <input type="checkbox" className="accent-accent" checked={albumArt} onChange={(e) => setAlbumArt(e.target.checked)} />
                    Also use for the album
                  </label>
                )}
                <p className="text-[11px] leading-snug text-subtle">
                  {[t.codec, t.bit_depth ? `${t.bit_depth}-bit` : "", t.sample_rate ? `${t.sample_rate / 1000} kHz` : "", t.bitrate ? `${Math.round(t.bitrate / 1000)} kbps` : ""]
                    .filter(Boolean)
                    .join(" · ")}
                </p>
              </div>
              <fieldset className="space-y-3" disabled={lockedNow}>
                <Field label="Title" hint={lockHint("title")}>
                  <Input value={form.title} onChange={(e) => set("title", e.target.value)} required />
                </Field>
                <Field label="Artists" hint={lockHint("artist") || "Separate several artists with commas."}>
                  <Input value={form.artist} onChange={(e) => set("artist", e.target.value)} />
                </Field>
                <Field label="Album" hint={lockHint("album") || "Links to an existing album with this name, or creates one."}>
                  <Input value={form.album} onChange={(e) => set("album", e.target.value)} />
                </Field>
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                  <Field label="Genre">
                    <Input value={form.genre} onChange={(e) => set("genre", e.target.value)} />
                  </Field>
                  <Field label="Year">
                    <Input value={form.year} onChange={(e) => set("year", e.target.value)} inputMode="numeric" />
                  </Field>
                  <Field label="Disc">
                    <Input value={form.disc} onChange={(e) => set("disc", e.target.value)} inputMode="numeric" />
                  </Field>
                  <Field label="Track">
                    <Input value={form.track} onChange={(e) => set("track", e.target.value)} inputMode="numeric" />
                  </Field>
                </div>
              </fieldset>
            </div>
            <fieldset className="space-y-3" disabled={lockedNow}>
              <div className="grid gap-3 sm:grid-cols-3">
                <Field label="ISRC">
                  <Input value={form.isrc} onChange={(e) => set("isrc", e.target.value)} />
                </Field>
                <Field label="MusicBrainz ID">
                  <Input value={form.mbid} onChange={(e) => set("mbid", e.target.value)} />
                </Field>
                <Field label="Volume adjust (dB)">
                  <Input value={form.gain} onChange={(e) => set("gain", e.target.value)} inputMode="decimal" placeholder="0" />
                </Field>
              </div>
              <Field label="Lyrics" hint={t.lyrics_timed ? "These lyrics are synced. Saving plain text replaces them with your version." : undefined}>
                <Textarea rows={6} value={form.lyrics} onChange={(e) => set("lyrics", e.target.value)} />
              </Field>
            </fieldset>
            <div className="grid gap-2 rounded-lg bg-surface-2/60 p-3 sm:grid-cols-2">
              <ToggleRow label="Explicit" checked={form.explicit} disabled={lockedNow} onChange={(v) => set("explicit", v)} />
              <ToggleRow label="Keep forever" hint="Never prune this song" checked={form.keepForever} onChange={(v) => set("keepForever", v)} />
              <ToggleRow
                label="Lock metadata"
                hint="Stops scans and refreshes changing it"
                icon={<Lock className="h-3.5 w-3.5" />}
                checked={form.locked}
                onChange={(v) => set("locked", v)}
              />
              <ToggleRow
                label="Write tags to file"
                hint={t.write_back_supported ? "Managed library" : "Managed libraries only"}
                checked={writeBack}
                disabled={!t.write_back_supported}
                onChange={setWriteBack}
              />
            </div>
            {lockedNow && <p className="text-xs text-subtle">Metadata is locked. Turn off the lock to edit fields.</p>}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={onClose} disabled={saving}>
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving && <Loader2 className="h-4 w-4 animate-spin" />}
                {saving ? "Saving…" : "Save"}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ToggleRow({
  label,
  hint,
  icon,
  checked,
  disabled,
  onChange
}: {
  label: string;
  hint?: string;
  icon?: React.ReactNode;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center justify-between gap-3 rounded-md px-1 py-1">
      <span className="min-w-0">
        <span className="flex items-center gap-1.5 text-sm">
          {icon}
          {label}
        </span>
        {hint && <span className="block text-[11px] text-subtle">{hint}</span>}
      </span>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onChange} />
    </label>
  );
}
