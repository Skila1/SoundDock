import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Mic2 } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/ui/empty";
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
  const enabled = !!q.data?.enabled;
  const url = q.data?.provider_url || "";

  async function save(next: LyricsProviderConfig) {
    setSaving(true);
    try {
      await api.put<LyricsProviderConfig>("/api/v1/admin/lyrics", next);
      toast.success("Lyrics provider saved");
      await qc.invalidateQueries({ queryKey: ["admin-lyrics"] });
    } catch (e) {
      toast.error((e as Error).message || "Could not save lyrics provider");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div>
      <PageHeader
        title="Lyrics"
        description="Optional LRCLIB lookup for tracks without embedded or manual lyrics. Off by default. Genius and Musixmatch are not used."
      />
      <article className="max-w-lg rounded-xl border border-border bg-surface-1 p-4 text-sm">
        <div className="mb-3 flex items-center gap-2 font-medium">
          <Mic2 className="h-4 w-4 text-muted" />
          Provider
        </div>
        <div className="mb-4 flex items-center gap-3">
          <Switch
            checked={enabled}
            onCheckedChange={(v) => {
              if (saving || q.isLoading) return;
              save({ enabled: v, provider_url: v ? url || lrclibURL : "" });
            }}
          />
          <div>
            <div className="font-medium">{enabled ? "Enabled" : "Disabled"}</div>
            <div className="text-xs text-subtle">Empty URL means no network requests.</div>
          </div>
        </div>
        <Field
          label="provider_url"
          hint="Allowlisted documented hosts only (lrclib.net). Unknown hosts are rejected."
        >
          <Input
            value={url}
            placeholder={lrclibURL}
            disabled={saving || q.isLoading}
            onChange={(e) => {
              qc.setQueryData<LyricsProviderConfig>(["admin-lyrics"], {
                enabled,
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
          onClick={() => save({ enabled, provider_url: enabled ? url || lrclibURL : "" })}
        >
          Save
        </Button>
      </article>
    </div>
  );
}
