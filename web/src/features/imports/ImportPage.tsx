import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { EmptyState } from "@/components/ui/empty";
import { toast } from "sonner";

export function ImportPage() {
  const jobs = useQuery({
    queryKey: ["import-jobs"],
    queryFn: () => api.get<any[]>("/api/v1/imports/jobs"),
    refetchInterval: 3000
  });
  const [url, setUrl] = useState("");
  const imports = jobs.data || [];

  return (
    <div>
      <PageHeader title="Remote Import" description="Paste a direct link to an audio file you already have the right to download. Streaming-service pages are not fetched." />
      <form
        className="max-w-xl space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          if (/open\.spotify\.com\/playlist|youtube\.com\/playlist|soundcloud\.com\/.+\/sets\/|music\.apple\.com\/.+\/playlist/.test(url)) {
            toast.error("Playlist URLs go to Playlists → Import from URL. Remote Import is for direct audio files only.");
            return;
          }
          try {
            await api.post("/api/v1/imports/url", { url });
            toast.success("Import started");
            setUrl("");
            jobs.refetch();
          } catch (err: any) {
            toast.error(err?.message || "Import failed");
          }
        }}
      >
        <Field label="Direct file URL" hint="HTTP(S) URL to an audio file (FLAC, MP3, M4A, Ogg, Opus, WAV).">
          <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/album/track.flac" required />
        </Field>
        <Button type="submit">Start import</Button>
      </form>
      <h2 className="mt-8 mb-3 font-semibold">Import jobs</h2>
      {!imports.length && <EmptyState icon={Globe} title="No import jobs yet." />}
      <ul className="space-y-2">
        {imports.map((j) => (
          <li key={j.id} className="flex items-center justify-between rounded-lg border border-border bg-surface-1 px-4 py-3">
            <div>
              <div className="text-sm font-medium">{j.type}</div>
              <div className="text-xs text-subtle">{j.last_error || (j.status === "completed" ? "Imported" : j.status || "Running")}</div>
            </div>
            <Badge tone={j.status === "failed" ? "danger" : j.status === "completed" ? "success" : "accent"}>{j.status}</Badge>
          </li>
        ))}
      </ul>
    </div>
  );
}
