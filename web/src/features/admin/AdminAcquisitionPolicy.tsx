import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";
import type { AcquisitionPolicy } from "@/types/api";

const profiles = [
  { value: "m4a-0", label: "AAC m4a-0 (default)" },
  { value: "mp3-0", label: "MP3 mp3-0" },
  { value: "opus-0", label: "Opus opus-0" }
];

export function AdminAcquisitionPolicy() {
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);
  const q = useQuery({
    queryKey: ["admin-acquisition-policy"],
    queryFn: () => api.get<AcquisitionPolicy>("/api/v1/admin/acquisition-policy")
  });
  const profile = q.data?.format_profile || "m4a-0";

  async function save(next: string) {
    setSaving(true);
    try {
      await api.put<AcquisitionPolicy>("/api/v1/admin/acquisition-policy", {
        media_policy_id: next,
        format_profile: next
      });
      toast.success("Acquisition policy saved");
      await qc.invalidateQueries({ queryKey: ["admin-acquisition-policy"] });
    } catch (e) {
      toast.error((e as Error).message || "Could not save policy");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div>
      <PageHeader
        title="Acquisition"
        description="Default format profile for YouTube and ScapeX restores. Jobs read this dest+policy. Raw yt-dlp arguments are not accepted."
      />
      <article className="max-w-lg rounded-xl border border-border bg-surface-1 p-4 text-sm">
        <div className="mb-3 flex items-center gap-2 font-medium">
          <Download className="h-4 w-4 text-muted" />
          Format profile
        </div>
        <Field label="media_policy_id / format_profile" hint="Allowlisted profiles only. yt-dlp flags are rejected by the API.">
          <Select
            value={profiles.some((p) => p.value === profile) ? profile : "m4a-0"}
            onValueChange={(v) => {
              if (!saving && !q.isLoading) save(v);
            }}
            options={profiles}
          />
        </Field>
        <p className="mt-3 text-xs text-subtle">Requires library.acquire. Changing the profile does not re-run in-flight downloads.</p>
        <Button className="mt-4" type="button" disabled={saving || q.isLoading} onClick={() => save(profile)}>
          Save
        </Button>
      </article>
    </div>
  );
}
