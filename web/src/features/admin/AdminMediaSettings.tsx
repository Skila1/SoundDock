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
import type { AcquisitionPolicy, LyricsProviderConfig } from "@/types/api";
import { Card, SettingSwitch, errorMessage } from "./adminUi";

export function AdminMediaSettings() {
  return (
    <div>
      <PageHeader title="Media Settings" description="Where SoundDock gets metadata and lyrics, the format it downloads in, and the transcode cache. Changes save as soon as you make them." />
      <div className="grid gap-5 lg:grid-cols-2">
        <MetadataSettings />
        <DownloadSettings />
        <LyricsSettings />
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
        description={`Used during scans and by Catalog → Cleanup. Sources: ${(q.data?.providers || []).join(", ") || "MusicBrainz, Cover Art Archive"}.`}
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
