import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, CircleAlert, FileAudio, Upload as Up } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/select";
import { Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import type { Library } from "@/types/api";
import { toast } from "sonner";

type Item = { file: File; status: "queued" | "uploading" | "done" | "error"; progress: number; error?: string };

export function UploadPage() {
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<Library[]>("/api/v1/libraries") });
  const writable = (libs.data || []).filter((l) => !l.read_only);
  const [lib, setLib] = useState("");
  const [items, setItems] = useState<Item[]>([]);
  const input = useRef<HTMLInputElement>(null);
  const folder = useRef<HTMLInputElement>(null);
  const dest = lib || writable[0]?.id || "";

  const addFiles = (files: FileList | File[]) => {
    const next = [...files].filter((f) => /\.(flac|mp3|m4a|ogg|opus|wav|aiff?)$/i.test(f.name));
    setItems((s) => [...s, ...next.map((file) => ({ file, status: "queued" as const, progress: 0 }))]);
  };

  const run = async () => {
    if (!dest) return toast.error("Choose a destination library");
    for (let i = 0; i < items.length; i++) {
      const it = items[i];
      if (it.status === "done") continue;
      setItems((s) => s.map((x, n) => (n === i ? { ...x, status: "uploading" } : x)));
      try {
        const created = await api.post<{ id: string }>(`/api/v1/uploads`, { filename: it.file.name, size: it.file.size, library_id: dest });
        const buf = await it.file.arrayBuffer();
        await fetch(`/api/v1/uploads/${created.id}`, {
          method: "PATCH",
          credentials: "include",
          headers: { "Upload-Offset": "0", "Content-Type": "application/offset+octet-stream" },
          body: buf
        });
        await api.post(`/api/v1/uploads/${created.id}/complete`);
        setItems((s) => s.map((x, n) => (n === i ? { ...x, status: "done", progress: 100 } : x)));
      } catch (e: any) {
        setItems((s) => s.map((x, n) => (n === i ? { ...x, status: "error", error: e.message } : x)));
      }
    }
    toast.success("Upload completed");
  };

  const overall = items.length ? items.reduce((s, x) => s + (x.status === "done" ? 100 : x.progress), 0) / items.length : 0;

  return (
    <div>
      <PageHeader title="Upload" description="Drop files into a writable library. SoundDock will scan tags and artwork after complete." />
      <div className="mb-4 max-w-sm">
        <Select value={dest} onValueChange={setLib} options={writable.map((l) => ({ value: l.id, label: l.name }))} placeholder="Destination library" />
      </div>
      <div
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => { e.preventDefault(); addFiles(e.dataTransfer.files); }}
        className="flex min-h-48 cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed border-border bg-surface-1 p-8 text-center"
        onClick={() => input.current?.click()}
      >
        <Up className="mb-2 h-8 w-8 text-accent" />
        <p className="font-medium">Drop audio files here</p>
        <p className="text-sm text-muted">FLAC, MP3, M4A, Ogg, Opus, WAV</p>
        <div className="mt-3 flex gap-2">
          <Button type="button" size="sm" onClick={(e) => { e.stopPropagation(); input.current?.click(); }}>Choose files</Button>
          <Button type="button" size="sm" variant="secondary" onClick={(e) => { e.stopPropagation(); folder.current?.click(); }}>Choose folder</Button>
        </div>
        <input ref={input} type="file" multiple hidden onChange={(e) => e.target.files && addFiles(e.target.files)} />
        <input ref={folder} type="file" multiple hidden {...({ webkitdirectory: "" } as any)} onChange={(e) => e.target.files && addFiles(e.target.files)} />
      </div>
      {items.length > 0 && (
        <div className="mt-6 rounded-xl border border-border bg-surface-1 p-4">
          <div className="mb-3 flex items-center justify-between">
            <span className="text-sm text-muted">{items.filter((i) => i.status === "done").length}/{items.length} complete</span>
            <Button onClick={run}>Start upload</Button>
          </div>
          <Progress value={overall} />
          <ul className="mt-4 space-y-2">
            {items.map((it, i) => (
              <li key={i} className="flex items-center gap-3 text-sm">
                {it.status === "done" ? <CheckCircle2 className="h-4 w-4 text-success" /> : it.status === "error" ? <CircleAlert className="h-4 w-4 text-destructive" /> : <FileAudio className="h-4 w-4 text-muted" />}
                <span className="flex-1 truncate">{it.file.name}</span>
                <span className="text-xs text-subtle">{it.status === "error" ? it.error : it.status}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
