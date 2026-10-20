import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { PageHeader, QueryError } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import type { AcquisitionPolicy, LyricsGeneratorStatus, LyricsProviderConfig } from "@/types/api";
import { Card, SettingSwitch, errorMessage } from "./adminUi";

export function AdminMediaSettings() {
  return (
    <div>
      <PageHeader title="Media Settings" description="Where SoundDock gets metadata and lyrics, the format it downloads in, and the transcode cache. Changes save as soon as you make them." />
      <div className="grid gap-5 lg:grid-cols-2">
        <MetadataSettings />
        <DownloadSettings />
        <LyricsSettings />
        <LyricsGeneratorSettings />
        <TranscodeSettings />
      </div>
    </div>
  );
}

function MetadataSettings() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-meta"], queryFn: () => api.get<{ external_enabled?: boolean; providers?: string[] }>("/api/v1/admin/metadata") });
  return (
    <Card title="Metadata" description="MusicBrainz fills in titles, artists, genres, and IDs; Cover Art Archive supplies album artwork.">
      <SettingSwitch
        label="Look up metadata online"
        description={`Used during scans and by Metadata cleanup on the Scan page. Sources: ${(q.data?.providers || []).join(", ") || "MusicBrainz, Cover Art Archive"}.`}
        checked={!!q.data?.external_enabled}
        disabled={q.isLoading}
        onChange={async (v) => {
          try {
            await api.put("/api/v1/admin/metadata", { external_enabled: v });
            toast.success(v ? "Online metadata on" : "Online metadata off");
            qc.invalidateQueries({ queryKey: ["admin-meta"] });
          } catch (e) {
            toast.error(errorMessage(e, "Could not save"));
          }
        }}
      />
    </Card>
  );
}

const profiles = [
  { value: "m4a-0", label: "AAC (.m4a), recommended" },
  { value: "mp3-0", label: "MP3" },
  { value: "opus-0", label: "Opus" }
];

function DownloadSettings() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-acquisition-policy"], queryFn: () => api.get<AcquisitionPolicy>("/api/v1/admin/acquisition-policy") });
  const profile = q.data?.format_profile || "m4a-0";
  return (
    <Card title="Downloads" description="The audio format used when SoundDock downloads music it does not have yet. Downloads already in progress keep their format.">
      <Field label="Download format">
        <Select
          value={profiles.some((p) => p.value === profile) ? profile : "m4a-0"}
          onValueChange={async (v) => {
            try {
              await api.put<AcquisitionPolicy>("/api/v1/admin/acquisition-policy", { media_policy_id: v, format_profile: v });
              toast.success("Download format saved");
              qc.invalidateQueries({ queryKey: ["admin-acquisition-policy"] });
            } catch (e) {
              toast.error(errorMessage(e, "Could not save format"));
            }
          }}
          options={profiles}
        />
      </Field>
    </Card>
  );
}

const lrclibURL = "https://lrclib.net";

function LyricsSettings() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-lyrics"], queryFn: () => api.get<LyricsProviderConfig>("/api/v1/admin/lyrics") });
  const localOn = q.data?.local_enabled !== false;
  const externalOn = !!(q.data?.external_enabled || q.data?.enabled);
  const savedURL = q.data?.provider_url || "";
  const [url, setUrl] = useState("");
  const [saving, setSaving] = useState(false);
  useEffect(() => { setUrl(savedURL); }, [savedURL]);

  async function save(next: { local?: boolean; external?: boolean; url?: string }) {
    const ext = next.external ?? externalOn;
    setSaving(true);
    try {
      await api.put<LyricsProviderConfig>("/api/v1/admin/lyrics", {
        ...q.data,
        local_enabled: next.local ?? localOn,
        external_enabled: ext,
        enabled: ext,
        provider_url: ext ? (next.url ?? url) || lrclibURL : ""
      });
      toast.success("Lyrics settings saved");
      await qc.invalidateQueries({ queryKey: ["admin-lyrics"] });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save lyrics settings"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card title="Lyrics" description="Lyrics you edit by hand always win and are never overwritten.">
      {q.isError && <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />}
      <div className="divide-y divide-border">
        <SettingSwitch
          label="Use lyrics stored with the music"
          description="Embedded tags first, then .lrc or .txt files in the lyrics folder."
          checked={localOn}
          disabled={saving || q.isLoading}
          onChange={(v) => void save({ local: v })}
        />
        <SettingSwitch
          label="Find missing lyrics on LRCLIB"
          description="Only asked when no local lyrics exist."
          checked={externalOn}
          disabled={saving || q.isLoading}
          onChange={(v) => void save({ external: v })}
        />
      </div>
      {externalOn && (
        <form
          className="mt-3 flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void save({ url });
          }}
        >
          <div className="min-w-0 flex-1">
            <Field label="LRCLIB address" hint="Only lrclib.net or a documented mirror is accepted.">
              <Input value={url} placeholder={lrclibURL} onChange={(e) => setUrl(e.target.value)} />
            </Field>
          </div>
          {url !== savedURL && <Button type="submit" disabled={saving}>Save</Button>}
        </form>
      )}
    </Card>
  );
}

const languages = [
  { value: "auto", label: "Detect for each song" },
  { value: "en", label: "English" },
  { value: "ar", label: "Arabic" },
  { value: "zh", label: "Chinese" },
  { value: "nl", label: "Dutch" },
  { value: "fr", label: "French" },
  { value: "de", label: "German" },
  { value: "hi", label: "Hindi" },
  { value: "it", label: "Italian" },
  { value: "ja", label: "Japanese" },
  { value: "ko", label: "Korean" },
  { value: "pt", label: "Portuguese" },
  { value: "ru", label: "Russian" },
  { value: "es", label: "Spanish" },
  { value: "tr", label: "Turkish" }
];

const generatorStatus: Record<LyricsGeneratorStatus["status"], { label: string; tone: string }> = {
  ok: { label: "Connected", tone: "bg-emerald-500" },
  loading: { label: "Loading its model", tone: "bg-amber-500" },
  unreachable: { label: "Not reachable", tone: "bg-red-500" },
  not_configured: { label: "Not set up", tone: "bg-muted" }
};

function LyricsGeneratorSettings() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-lyrics"], queryFn: () => api.get<LyricsProviderConfig>("/api/v1/admin/lyrics") });
  const status = useQuery({
    queryKey: ["admin-lyrics-generator"],
    queryFn: () => api.get<LyricsGeneratorStatus>("/api/v1/admin/lyrics/generator"),
    refetchInterval: (query) => (query.state.data?.status === "loading" ? 5000 : false)
  });
  const [saving, setSaving] = useState(false);
  const on = !!q.data?.generate_enabled;
  const language = q.data?.generate_language || "auto";
  const st = generatorStatus[status.data?.status || "not_configured"];

  async function save(next: Partial<Pick<LyricsProviderConfig, "generate_enabled" | "generate_language">>) {
    setSaving(true);
    try {
      await api.put<LyricsProviderConfig>("/api/v1/admin/lyrics", { ...q.data, ...next });
      toast.success("Lyrics generation saved");
      await qc.invalidateQueries({ queryKey: ["admin-lyrics"] });
      await qc.invalidateQueries({ queryKey: ["track-lyrics"] });
    } catch (e) {
      toast.error(errorMessage(e, "Could not save lyrics generation"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card
      title="Lyrics generation"
      description={
        <>
          Writes timed lyrics for songs no lyrics service knows, such as your own music, by listening to the audio with{" "}
          <a className="underline underline-offset-2" href="https://github.com/ggml-org/whisper.cpp" target="_blank" rel="noreferrer">whisper.cpp</a>.
          Lyrics you edit by hand are never replaced.
        </>
      }
    >
      <div className="mb-2 flex items-center gap-2 text-sm">
        <span className={`h-2 w-2 rounded-full ${st.tone}`} aria-hidden />
        <span className="font-medium">{st.label}</span>
        {status.data?.url && <span className="truncate text-xs text-subtle">{status.data.url}</span>}
        <Button size="sm" variant="ghost" className="ml-auto" disabled={status.isFetching} onClick={() => status.refetch()}>Check again</Button>
      </div>
      {status.data?.status === "not_configured" && (
        <p className="mb-2 text-xs text-muted">
          Start the whisper container with <code>docker compose --profile lyrics up -d</code> and set <code>SD_WHISPER_URL=http://whisper:8080</code> in <code>.env</code>, then restart SoundDock.
        </p>
      )}
      <div className="divide-y divide-border">
        <SettingSwitch
          label="Allow generating lyrics"
          description="Admins get a Generate lyrics button in the lyrics panel for songs without lyrics."
          checked={on}
          disabled={saving || q.isLoading}
          onChange={(v) => void save({ generate_enabled: v })}
        />
      </div>
      {on && (
        <div className="mt-3">
          <Field label="Language" hint="Picking the language helps with accents and mixed-language songs.">
            <Select value={languages.some((l) => l.value === language) ? language : "auto"} onValueChange={(v) => void save({ generate_language: v })} options={languages} />
          </Field>
        </div>
      )}
    </Card>
  );
}

function humanKey(k: string) {
  const s = k.replaceAll("_", " ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function TranscodeSettings() {
  const q = useQuery({ queryKey: ["tx"], queryFn: () => api.get<Record<string, unknown>>("/api/v1/admin/transcode") });
  const [confirm, setConfirm] = useState(false);
  const rows = Object.entries(q.data || {}).filter(([, v]) => typeof v !== "object");
  return (
    <Card
      title="Transcoding"
      description="Converted copies made for devices that cannot play the original format."
      actions={<Button size="sm" variant="secondary" onClick={() => setConfirm(true)}>Clear cache</Button>}
    >
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted">{humanKey(k)}</dt>
            <dd className="font-medium">{String(v)}</dd>
          </div>
        ))}
        {!rows.length && <dd className="col-span-2 text-muted">{q.isLoading ? "Loading…" : "No transcode information."}</dd>}
      </dl>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Clear the transcode cache?"
        description="Converted copies are deleted and made again the next time someone plays them."
        confirmLabel="Clear cache"
        onConfirm={async () => {
          try {
            await api.del("/api/v1/admin/transcode/cache");
            toast.success("Cache cleared");
            q.refetch();
          } catch (e) {
            toast.error(errorMessage(e, "Could not clear cache"));
          }
        }}
      />
    </Card>
  );
}
