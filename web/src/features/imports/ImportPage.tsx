import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { EmptyState } from "@/components/ui/empty";
import type { Library } from "@/types/api";
import { toast } from "sonner";

export function ImportPage() {
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const jobs = useQuery({ queryKey: ["jobs"], queryFn: () => api.get<any[]>("/api/v1/admin/jobs"), retry: false });
  const writable = (libs.data || []).filter((l) => !l.read_only);
  const [url, setUrl] = useState("");
  const [lib, setLib] = useState("");
  const dest = lib || writable[0]?.id || "";
  const imports = (jobs.data || []).filter((j) => String(j.type || "").includes("import"));

  return (
    <div>
      <PageHeader title="Remote Import" description="Import audio you already have the right to download. SoundDock will not rip streaming services." />
      <form
        className="max-w-xl space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          if (/open\.spotify\.com\/playlist|youtube\.com\/playlist|soundcloud\.com\/.+\/sets\/|music\.apple\.com\/.+\/playlist/.test(url)) {
            toast.error("Playlist URLs go to Playlists → Import from URL. Remote Import is for direct audio files only.");
            return;
          }
          await api.post("/api/v1/imports/url", { url, library_id: dest });
          toast.success("Import started");
          setUrl("");
        }}
      >
        <Field label="Destination library">
          <Select value={dest} onValueChange={setLib} options={writable.map((l) => ({ value: l.id, label: l.name }))} />
        </Field>
        <Field label="Direct file URL" hint="HTTP(S) URL to an audio file you host or are permitted to fetch.">
          <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/album/track.flac" required />
        </Field>
        <Button type="submit">Start import</Button>
      </form>
      <h2 className="mt-8 mb-3 font-semibold">Active jobs</h2>
      {!imports.length && <EmptyState icon={Globe} title="No import jobs yet." />}
      <ul className="space-y-2">
        {imports.map((j) => (
          <li key={j.id} className="flex items-center justify-between rounded-lg border border-border bg-surface-1 px-4 py-3">
            <div>
              <div className="text-sm font-medium">{j.type}</div>
              <div className="text-xs text-subtle">{j.last_error || "Running"}</div>
            </div>
            <Badge tone={j.status === "failed" ? "danger" : j.status === "succeeded" ? "success" : "accent"}>{j.status}</Badge>
          </li>
        ))}
      </ul>
    </div>
  );
}
