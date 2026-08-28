import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Mic2 } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { PageHeader, QueryError } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import type { LyricsProviderConfig } from "@/types/api";

const lrclibURL = "https://lrclib.net";

export function AdminLyrics() {
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);
  const q = useQuery({
    queryKey: ["admin-lyrics"],
    queryFn: () => api.get<LyricsProviderConfig>("/api/v1/admin/lyrics")
  });
  const localOn = q.data?.local_enabled !== false;
  const externalOn = !!(q.data?.external_enabled || q.data?.enabled);
  const url = q.data?.provider_url || "";

  async function save(next: LyricsProviderConfig) {
    setSaving(true);
    try {
      await api.put<LyricsProviderConfig>("/api/v1/admin/lyrics", next);
      toast.success("Lyrics settings saved");
      await qc.invalidateQueries({ queryKey: ["admin-lyrics"] });
    } catch (e) {
      toast.error((e as Error).message || "Could not save lyrics settings");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div>
      <PageHeader
        title="Lyrics"
        description="Local lyrics (embedded tags and files under data/lyrics) are the default. LRCLIB is optional and off until you turn it on."
      />
      {q.isError && <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />}
      <article className="mb-4 max-w-lg rounded-xl border border-border bg-surface-1 p-4 text-sm">
        <div className="mb-3 flex items-center gap-2 font-medium">
          <Mic2 className="h-4 w-4 text-muted" />
          Local lyrics
        </div>
        <div className="flex items-center gap-3">
          <Switch
            checked={localOn}
            onCheckedChange={(v) => {
              if (saving || q.isLoading) return;
              save({ local_enabled: v, external_enabled: externalOn, enabled: externalOn, provider_url: externalOn ? url || lrclibURL : "" });
            }}
          />
          <div>
            <div className="font-medium">{localOn ? "On" : "Off"}</div>
            <div className="text-xs text-subtle">Reads embedded tags first, then data/lyrics/artist/title.lrc (or .txt). Manual edits always win.</div>
          </div>
        </div>
      </article>
      <article className="max-w-lg rounded-xl border border-border bg-surface-1 p-4 text-sm">
        <div className="mb-3 font-medium">External provider (optional)</div>
        <div className="mb-4 flex items-center gap-3">
          <Switch
            checked={externalOn}
            onCheckedChange={(v) => {
              if (saving || q.isLoading) return;
              save({ local_enabled: localOn, external_enabled: v, enabled: v, provider_url: v ? url || lrclibURL : "" });
            }}
          />
          <div>
            <div className="font-medium">{externalOn ? "LRCLIB on" : "LRCLIB off"}</div>
            <div className="text-xs text-subtle">Used only when local lyrics are missing. Cached LRCLIB lyrics are not shown while this is off. Genius and Musixmatch are not used.</div>
          </div>
        </div>
        <Field
          label="provider_url"
          hint="Allowlisted documented hosts only (lrclib.net). Unknown hosts are rejected."
        >
          <Input
            value={url}
            placeholder={lrclibURL}
            disabled={saving || q.isLoading || !externalOn}
            onChange={(e) => {
              qc.setQueryData<LyricsProviderConfig>(["admin-lyrics"], {
                local_enabled: localOn,
                external_enabled: externalOn,
                enabled: externalOn,
                provider_url: e.target.value
              });
            }}
          />
        </Field>
        <p className="mt-3 text-xs text-subtle">Requires lyrics.configure. Manual and metadata-locked lyrics are never overwritten.</p>
        <Button
          className="mt-4"
          type="button"
          disabled={saving || q.isLoading}
          onClick={() => save({ local_enabled: localOn, external_enabled: externalOn, enabled: externalOn, provider_url: externalOn ? url || lrclibURL : "" })}
        >
          Save
        </Button>
      </article>
    </div>
  );
}
