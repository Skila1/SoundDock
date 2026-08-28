import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { toast } from "sonner";
import { hasPerm } from "@/lib/perms";
import type { User } from "@/types/api";

function splitURLs(raw: string) {
  return raw.split(/[\n,]+/).map((s) => s.trim()).filter((s) => s && !s.startsWith("#"));
}

export function ImportPage() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const jobs = useQuery({
    queryKey: ["import-jobs"],
    queryFn: () => api.get<any[]>("/api/v1/imports/jobs"),
    refetchInterval: 3000
  });
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const imports = jobs.data || [];
  const count = splitURLs(url).length;
  const canImport = hasPerm(me.data, "library.import_url") || hasPerm(me.data, "library.upload");

  if (jobs.isError) {
    return <QueryError message={jobs.error instanceof Error ? jobs.error.message : undefined} onRetry={() => jobs.refetch()} />;
  }

  return (
    <div>
      <PageHeader title="Import" description="Paste one or many direct audio or zip URLs, one per line. Streaming-service pages are not fetched." />
      {!canImport && !me.isLoading && (
        <p className="mb-4 text-sm text-muted">You do not have permission to import files.</p>
      )}
      {canImport && <form
        className="max-w-xl space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          const urls = splitURLs(url);
          if (!urls.length) return toast.error("Paste at least one audio URL");
          if (urls.some((u) => /open\.spotify\.com\/playlist|youtube\.com\/playlist|soundcloud\.com\/.+\/sets\/|music\.apple\.com\/.+\/playlist/.test(u))) {
            toast.error("Playlist URLs go to Playlists → Import from URL. Import is for direct audio files only.");
            return;
          }
          setBusy(true);
          try {
            const res = await api.post<{ count: number }>("/api/v1/imports/url", { urls });
            toast.success(res.count > 1 ? `Importing ${res.count} files` : "Import started");
            setUrl("");
            jobs.refetch();
          } catch (err: any) {
            toast.error(err?.message || "Import failed");
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="Direct file URLs" hint="One HTTP(S) audio or zip URL per line. Commas work too. Up to 200.">
          <Textarea
            className="min-h-36 font-mono text-xs"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder={"https://example.com/album/01-track.flac\nhttps://example.com/album/02-track.flac"}
            required
          />
        </Field>
        <Button type="submit" disabled={busy}>{busy ? "Starting…" : count > 1 ? `Import ${count} files` : "Start import"}</Button>
      </form>}
      <h2 className="mt-8 mb-3 font-semibold">Import jobs</h2>
      {!imports.length && <EmptyState icon={Globe} title="No import jobs yet." />}
      <ul className="space-y-2">
        {imports.map((j) => (
          <li key={j.id} className="flex items-center justify-between rounded-lg border border-border bg-surface-1 px-4 py-3">
            <div>
              <div className="text-sm font-medium">{j.count > 1 ? `${j.count} URLs` : j.type}</div>
              <div className="text-xs text-subtle">{j.last_error || (j.status === "completed" ? "Imported" : j.status === "running" && j.progress ? `${j.progress}%` : j.status || "Running")}</div>
            </div>
            <Badge tone={j.status === "failed" ? "danger" : j.status === "completed" ? "success" : "accent"}>{j.status}</Badge>
          </li>
        ))}
      </ul>
    </div>
  );
}
