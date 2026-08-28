import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CircleAlert, FileAudio, Upload as Up, X } from "lucide-react";
import { api } from "@/lib/api";
import { UPLOAD_ACCEPT, isBulkUploadFile, isZipFile } from "@/lib/audio";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";

type Item = { file: File; status: "queued" | "uploading" | "done" | "error"; progress: number; error?: string };

const CHUNK = 8 * 1024 * 1024;
const WORKERS = 100;

function fileLabel(file: File) {
  return (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name;
}

function patchChunk(id: string, offset: number, blob: Blob, onByte: (n: number) => void) {
  return new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PATCH", `/api/v1/uploads/${id}`);
    xhr.withCredentials = true;
    xhr.setRequestHeader("Upload-Offset", String(offset));
    xhr.setRequestHeader("Content-Type", "application/offset+octet-stream");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onByte(e.loaded);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(xhr.statusText || "upload failed"));
    };
    xhr.onerror = () => reject(new Error("upload failed"));
    xhr.send(blob);
  });
}

async function uploadOne(file: File, onProgress: (pct: number) => void) {
  const created = await api.post<{ id: string }>("/api/v1/uploads", { filename: file.name, size: file.size });
  let offset = 0;
  while (offset < file.size) {
    const chunk = file.slice(offset, offset + CHUNK);
    await patchChunk(created.id, offset, chunk, (loaded) => {
      onProgress(Math.min(99, Math.round(((offset + loaded) / file.size) * 100)));
    });
    offset += chunk.size;
  }
  await api.post(`/api/v1/uploads/${created.id}/complete`, { scan: !isZipFile(file) ? false : true });
  onProgress(100);
}

export function UploadPage() {
  const qc = useQueryClient();
  const [items, setItems] = useState<Item[]>([]);
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const folder = useRef<HTMLInputElement>(null);

  const addFiles = (files: FileList | File[]) => {
    const all = [...files];
    const next = all.filter(isBulkUploadFile);
    const skipped = all.length - next.length;
    if (skipped) toast.error(`Skipped ${skipped} file${skipped === 1 ? "" : "s"} that are not audio or zip`);
    if (!next.length) return;
    setItems((s) => [...s, ...next.map((file) => ({ file, status: "queued" as const, progress: 0 }))]);
  };

  const run = async () => {
    const pending = items.map((it, i) => ({ it, i })).filter(({ it }) => it.status !== "done");
    if (!pending.length) return toast.error("Add audio files or a zip first");
    setBusy(true);
    let cursor = 0;
    const worker = async () => {
      for (;;) {
        const next = cursor++;
        if (next >= pending.length) return;
        const { it, i } = pending[next];
        setItems((s) => s.map((x, n) => (n === i ? { ...x, status: "uploading", progress: 0 } : x)));
        try {
          await uploadOne(it.file, (pct) => {
            setItems((s) => s.map((x, n) => (n === i ? { ...x, progress: pct } : x)));
          });
          setItems((s) => s.map((x, n) => (n === i ? { ...x, status: "done", progress: 100 } : x)));
        } catch (e: any) {
          setItems((s) => s.map((x, n) => (n === i ? { ...x, status: "error", error: e.message } : x)));
        }
      }
    };
    try {
      await Promise.all(Array.from({ length: Math.min(WORKERS, pending.length) }, () => worker()));
      await api.post("/api/v1/uploads/finalize");
      qc.invalidateQueries({ queryKey: ["home"] });
      qc.invalidateQueries({ queryKey: ["libraries"] });
      qc.invalidateQueries({ queryKey: ["tracks"] });
      qc.invalidateQueries({ queryKey: ["albums"] });
      qc.invalidateQueries({ queryKey: ["artists"] });
      toast.success("Uploaded. SoundDock is adding tracks to your library.");
    } catch (e: any) {
      toast.error(e?.message || "Upload failed");
    } finally {
      setBusy(false);
    }
  };

  const overall = items.length ? items.reduce((s, x) => s + (x.status === "done" ? 100 : x.progress), 0) / items.length : 0;

  return (
    <div>
      <PageHeader title="Add music" description="Drop a folder, many files, or a zip. Up to 100 files upload at once. WAV and AIFF are stored as FLAC." />
      <div
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => { e.preventDefault(); addFiles(e.dataTransfer.files); }}
        className="flex min-h-48 cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed border-border bg-surface-1 p-8 text-center"
        onClick={() => input.current?.click()}
      >
        <Up className="mb-2 h-8 w-8 text-accent" />
        <p className="font-medium">Drop audio files, folders, or a zip</p>
        <p className="text-sm text-muted">FLAC, MP3, M4A, Ogg, Opus, WAV, AIFF, ZIP. Uncompressed files are compressed automatically.</p>
        <div className="mt-3 flex gap-2">
          <Button type="button" size="sm" onClick={(e) => { e.stopPropagation(); input.current?.click(); }}>Choose files</Button>
          <Button type="button" size="sm" variant="secondary" onClick={(e) => { e.stopPropagation(); folder.current?.click(); }}>Choose folder</Button>
        </div>
        <input ref={input} type="file" multiple hidden accept={UPLOAD_ACCEPT} onChange={(e) => e.target.files && addFiles(e.target.files)} />
        <input ref={folder} type="file" multiple hidden accept={UPLOAD_ACCEPT} {...({ webkitdirectory: "" } as any)} onChange={(e) => e.target.files && addFiles(e.target.files)} />
      </div>
      {items.length > 0 && (
        <div className="mt-6 rounded-xl border border-border bg-surface-1 p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <span className="text-sm text-muted">{items.filter((i) => i.status === "done").length}/{items.length} complete</span>
            <div className="flex gap-2">
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => setItems([])}>Clear</Button>
              <Button disabled={busy} onClick={run}>{busy ? "Uploading…" : "Start upload"}</Button>
            </div>
          </div>
          <Progress value={overall} />
          <ul className="mt-4 max-h-80 space-y-2 overflow-y-auto">
            {items.map((it, i) => (
              <li key={i} className="flex items-center gap-3 text-sm">
                {it.status === "done" ? <CheckCircle2 className="h-4 w-4 text-success" /> : it.status === "error" ? <CircleAlert className="h-4 w-4 text-destructive" /> : <FileAudio className="h-4 w-4 text-muted" />}
                <span className="min-w-0 flex-1 truncate">{fileLabel(it.file)}</span>
                <span className="text-xs text-subtle">{it.status === "error" ? it.error : it.status === "uploading" ? `${it.progress}%` : it.status}</span>
                {it.status === "queued" && (
                  <button type="button" className="text-subtle hover:text-foreground" onClick={() => setItems((s) => s.filter((_, n) => n !== i))} aria-label="Remove">
                    <X className="h-4 w-4" />
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
