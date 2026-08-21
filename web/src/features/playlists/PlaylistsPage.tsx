import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ListMusic } from "lucide-react";
import { api } from "@/lib/api";
import { MediaCard } from "@/components/media/MediaCard";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import type { Playlist } from "@/types/api";
import { toast } from "sonner";

const tabs = [
  { id: "all", label: "SoundDock" },
  { id: "spotify", label: "Spotify" },
  { id: "youtube", label: "YouTube" },
  { id: "soundcloud", label: "SoundCloud" },
  { id: "apple_music", label: "Apple Music" }
];

export function PlaylistsPage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["playlists"], queryFn: () => api.get<Playlist[]>("/api/v1/playlists") });
  const [open, setOpen] = useState(false);
  const [imp, setImp] = useState(false);
  const [tab, setTab] = useState("all");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [url, setUrl] = useState("");
  const [mode, setMode] = useState("once");
  const [interval, setInterval] = useState("6h");
  const list = (q.data || []).filter((p) => (tab === "all" ? !p.provider : p.provider === tab));

  return (
    <div>
      <PageHeader
        title="Playlists"
        actions={
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => setImp(true)}>Import from URL</Button>
            <Button onClick={() => setOpen(true)}>New playlist</Button>
          </div>
        }
      />
      <div className="mb-5 flex flex-wrap gap-1">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`rounded-full px-3 py-1 text-sm ${tab === t.id ? "bg-accent text-[#04140a]" : "bg-surface-2 text-muted"}`}
          >
            {t.label}
          </button>
        ))}
      </div>
      {!q.isLoading && !list.length && (
        <EmptyState
          icon={ListMusic}
          title={tab === "all" ? "Create your first playlist." : `No ${tabs.find((x) => x.id === tab)?.label} playlists yet.`}
          action={{ label: tab === "all" ? "New playlist" : "Import from URL", onClick: () => (tab === "all" ? setOpen(true) : setImp(true)) }}
        />
      )}
      <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
        {list.map((p) => (
          <div key={p.id} className="relative">
            <MediaCard className="max-w-none min-w-0" to={`/playlists/${p.id}`} id={p.id} title={p.name} kind="playlist" />
            {p.provider && <Badge className="absolute left-2 top-2" tone="accent">{p.provider.replace("_", " ")}</Badge>}
          </div>
        ))}
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="Create playlist">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await api.post("/api/v1/playlists", { name, description: desc });
              toast.success("Playlist created");
              setOpen(false);
              setName("");
              qc.invalidateQueries({ queryKey: ["playlists"] });
            }}
          >
            <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} required /></Field>
            <Field label="Description"><Textarea value={desc} onChange={(e) => setDesc(e.target.value)} /></Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
              <Button type="submit">Create</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={imp} onOpenChange={setImp}>
        <DialogContent title="Import playlist">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await api.post("/api/v1/providers/import-url", { url, mode, sync_interval: interval, removal_policy: "mirror" });
              toast.success("Import queued — matching against your SoundDock library");
              setImp(false);
              setUrl("");
              qc.invalidateQueries({ queryKey: ["playlists"] });
            }}
          >
            <Field label="Playlist URL" hint="Spotify, YouTube, SoundCloud, or Apple Music playlist. This does not download audio.">
              <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://open.spotify.com/playlist/…" required />
            </Field>
            <Field label="Mode">
              <Select value={mode} onValueChange={setMode} options={[
                { value: "once", label: "Import once" },
                { value: "sync", label: "Keep synced" },
                { value: "manual", label: "Manual only" }
              ]} />
            </Field>
            {mode === "sync" && (
              <Field label="Interval">
                <Select value={interval} onValueChange={setInterval} options={[
                  { value: "1h", label: "Hourly" },
                  { value: "6h", label: "Every 6 hours" },
                  { value: "12h", label: "Every 12 hours" },
                  { value: "24h", label: "Daily" }
                ]} />
              </Field>
            )}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setImp(false)}>Cancel</Button>
              <Button type="submit">Import</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
