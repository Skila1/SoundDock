import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ListMusic, Radio as RadioIcon } from "lucide-react";
import { api } from "@/lib/api";
import { hasPerm } from "@/lib/perms";
import { MediaCard } from "@/components/media/MediaCard";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";
import { usePlayer } from "@/stores/player";
import type { User } from "@/types/api";
import type { PlaylistFolder, PlaylistListItem, ProviderCapabilities, ProviderPlaylist, RadioResponse } from "./types";
import { capabilityBlurb } from "./types";

const allTabs = [
  { id: "all", label: "SoundDock" },
  { id: "spotify", label: "Spotify" },
  { id: "youtube", label: "YouTube" },
  { id: "soundcloud", label: "SoundCloud" },
  { id: "apple_music", label: "Apple Music" }
];

export function PlaylistsPage() {
  const qc = useQueryClient();
  const play = usePlayer((s) => s.playTracks);
  const [open, setOpen] = useState(false);
  const [imp, setImp] = useState(false);
  const [smart, setSmart] = useState(false);
  const [tab, setTab] = useState("all");
  const [folder, setFolder] = useState("all");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [newFolder, setNewFolder] = useState("");
  const [url, setUrl] = useState("");
  const [mode, setMode] = useState("once");
  const [interval, setInterval] = useState("6h");
  const [genre, setGenre] = useState("");
  const [ymin, setYmin] = useState("");
  const [ymax, setYmax] = useState("");
  const [busyId, setBusyId] = useState("");
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const canImport = hasPerm(me.data, "playlists.write") || hasPerm(me.data, "playlists.external_import");
  const q = useQuery({ queryKey: ["playlists"], queryFn: () => api.get<PlaylistListItem[]>("/api/v1/playlists") });
  const folders = useQuery({ queryKey: ["playlist-folders"], queryFn: () => api.get<PlaylistFolder[]>("/api/v1/playlists/folders") });
  const providers = useQuery({ queryKey: ["me-providers"], queryFn: () => api.get<{ provider: string; connected?: boolean; enabled?: boolean; configured?: boolean; status?: string; capabilities?: ProviderCapabilities }[]>("/api/v1/me/providers") });
  const connected = useMemo(() => {
    const set = new Set<string>();
    for (const p of providers.data || []) {
      if (p.connected && p.enabled !== false) set.add(p.provider);
    }
    return set;
  }, [providers.data]);
  const tabs = useMemo(() => allTabs.filter((t) => t.id === "all" || connected.has(t.id)), [connected]);
  const providerOk = tab !== "all" && connected.has(tab);
  useEffect(() => {
    if (tab !== "all" && !connected.has(tab) && providers.data) setTab("all");
  }, [tab, connected, providers.data]);
  const remote = useQuery({
    queryKey: ["provider-playlists", tab],
    enabled: providerOk,
    queryFn: () => api.get<ProviderPlaylist[]>(`/api/v1/providers/${tab}/playlists`)
  });

  const list = useMemo(() => {
    return (q.data || []).filter((p) => {
      if (tab === "all" ? p.provider : p.provider !== tab) {
        if (tab !== "all") return false;
        if (p.provider) return false;
      }
      if (folder !== "all" && (p.folder || "") !== folder) return false;
      return true;
    });
  }, [q.data, tab, folder]);

  const grouped = useMemo(() => {
    const map = new Map<string, PlaylistListItem[]>();
    for (const p of list) {
      const key = p.folder || "";
      const arr = map.get(key) || [];
      arr.push(p);
      map.set(key, arr);
    }
    return [...map.entries()];
  }, [list]);

  const quickMix = async () => {
    const r = await api.get<RadioResponse>("/api/v1/radio?kind=quick_mix&limit=20");
    const ids = r.track_ids || [];
    if (!ids.length) {
      toast.message("Quick Mix needs tracks in your library.");
      return;
    }
    await play(ids);
  };

  return (
    <div>
      <PageHeader
        title="Playlists"
        actions={
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onClick={() => { location.href = "/radio"; }}><RadioIcon /> Radio</Button>
            <Button variant="secondary" onClick={quickMix}>Quick Mix</Button>
            {canImport && <Button variant="secondary" onClick={() => setImp(true)}>Import from URL</Button>}
            {canImport && providerOk && (
              <Button
                variant="secondary"
                onClick={async () => {
                  setBusyId("all");
                  try {
                    const r = await api.post<{ count: number }>(`/api/v1/providers/${tab}/import-all`, { mode: "once" });
                    toast.success(`Queued ${r.count} playlists. Matching your library first.`);
                    qc.invalidateQueries({ queryKey: ["playlists"] });
                  } catch (err: any) {
                    toast.error(err?.message || "Could not import Spotify playlists");
                  } finally {
                    setBusyId("");
                  }
                }}
                disabled={busyId === "all"}
              >
                {busyId === "all" ? "Importing…" : `Import all from ${tabs.find((x) => x.id === tab)?.label || "provider"}`}
              </Button>
            )}
            <Button variant="secondary" onClick={() => setSmart(true)}>Smart playlist</Button>
            <Button onClick={() => setOpen(true)}>New playlist</Button>
          </div>
        }
      />
      <div className="mb-5 flex flex-wrap gap-1">
        {tabs.map((t) => {
          const row = (providers.data || []).find((p) => p.provider === t.id);
          return (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={`rounded-full px-3 py-1 text-sm ${tab === t.id ? "bg-accent text-[#04140a]" : "bg-surface-2 text-muted"}`}
            >
              {t.label}
              {row?.status === "needs_reconnect" ? " (reconnect)" : ""}
            </button>
          );
        })}
      </div>
      <div className="mb-5 flex flex-wrap gap-1">
        <button onClick={() => setFolder("all")} className={`rounded-full px-3 py-1 text-sm ${folder === "all" ? "bg-surface-3" : "bg-surface-2 text-muted"}`}>All folders</button>
        {(folders.data || []).filter((f) => f.name).map((f) => (
          <button key={f.name} onClick={() => setFolder(f.name)} className={`rounded-full px-3 py-1 text-sm ${folder === f.name ? "bg-surface-3" : "bg-surface-2 text-muted"}`}>
            {f.name} ({f.count})
          </button>
        ))}
      </div>
      {tab !== "all" && !providerOk && providers.data && (
        <p className="mb-5 text-sm text-muted">
          Connect this service in <a className="text-accent underline" href="/settings/connected">Connected Services</a>, or paste a playlist URL. SoundDock matches your library first.
        </p>
      )}
      {providerOk && (
        <p className="mb-4 text-sm text-muted">
          {capabilityBlurb((providers.data || []).find((p) => p.provider === tab)?.capabilities) || "Import playlists into SoundDock. Matching your library first."}
          {(providers.data || []).find((p) => p.provider === tab)?.status === "needs_reconnect" ? (
            <span className="ml-2"><Badge tone="warning">Needs reconnect</Badge></span>
          ) : null}
        </p>
      )}
      {canImport && providerOk && (remote.data || []).length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 text-sm font-medium uppercase tracking-wide text-subtle">Your {tabs.find((x) => x.id === tab)?.label} playlists</h2>
          <ul className="divide-y divide-border rounded-xl border border-border bg-surface-1">
            {(remote.data || []).map((pl) => (
              <li key={pl.id} className="flex items-center justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <div className="truncate font-medium">{pl.name}</div>
                  <div className="text-xs text-muted">{pl.track_count ?? 0} tracks{pl.owner ? ` · ${pl.owner}` : ""}</div>
                </div>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={busyId === pl.id}
                  onClick={async () => {
                    setBusyId(pl.id);
                    try {
                      await api.post(`/api/v1/providers/${tab}/playlists/${pl.id}/import`, { mode: "once", name: pl.name });
                      toast.success("Import queued. Matching your library first.");
                      qc.invalidateQueries({ queryKey: ["playlists"] });
                    } catch (err: any) {
                      toast.error(err?.message || "Import failed");
                    } finally {
                      setBusyId("");
                    }
                  }}
                >
                  {busyId === pl.id ? "Queued…" : "Import"}
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {!q.isLoading && !list.length && tab === "all" && (
        <EmptyState
          icon={ListMusic}
          title={tab === "all" ? "Create your first playlist." : `No ${tabs.find((x) => x.id === tab)?.label} playlists yet.`}
          action={{ label: tab === "all" ? "New playlist" : "Import from URL", onClick: () => (tab === "all" ? setOpen(true) : setImp(true)) }}
        />
      )}
      {grouped.map(([folderName, items]) => (
        <section key={folderName || "_"} className="mb-8">
          {folder === "all" && folderName && <h2 className="mb-3 text-sm font-medium uppercase tracking-wide text-subtle">{folderName}</h2>}
          <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {items.map((p) => (
              <div key={p.id} className="relative">
                <MediaCard className="max-w-none min-w-0" to={`/playlists/${p.id}`} id={p.id} title={p.name} kind="playlist" />
                {p.provider && <Badge className="absolute left-2 top-2" tone="accent">{p.provider.replace("_", " ")}</Badge>}
                {p.is_smart && <Badge className="absolute right-2 top-2" tone="neutral">Smart</Badge>}
              </div>
            ))}
          </div>
        </section>
      ))}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="Create playlist">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await api.post("/api/v1/playlists", { name, description: desc, folder: newFolder });
              toast.success("Playlist created");
              setOpen(false);
              setName("");
              setNewFolder("");
              qc.invalidateQueries({ queryKey: ["playlists"] });
              qc.invalidateQueries({ queryKey: ["playlist-folders"] });
            }}
          >
            <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} required /></Field>
            <Field label="Description"><Textarea value={desc} onChange={(e) => setDesc(e.target.value)} /></Field>
            <Field label="Folder"><Input value={newFolder} onChange={(e) => setNewFolder(e.target.value)} placeholder="Optional" /></Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
              <Button type="submit">Create</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={smart} onOpenChange={setSmart}>
        <DialogContent title="Smart playlist">
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              const clauses: { field: string; op: string; value: unknown }[] = [];
              if (genre) clauses.push({ field: "genre", op: "eq", value: genre });
              if (ymin) clauses.push({ field: "year", op: "gte", value: Number(ymin) });
              if (ymax) clauses.push({ field: "year", op: "lt", value: Number(ymax) });
              await api.post("/api/v1/playlists", {
                name: name || "Smart playlist",
                description: desc,
                folder: newFolder,
                smart: { limit: 50, match: "all", sort: "random", clauses }
              });
              toast.success("Smart playlist created");
              setSmart(false);
              setName("");
              qc.invalidateQueries({ queryKey: ["playlists"] });
            }}
          >
            <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="90s Rock" /></Field>
            <Field label="Genre"><Input value={genre} onChange={(e) => setGenre(e.target.value)} placeholder="Rock" /></Field>
            <div className="grid grid-cols-2 gap-2">
              <Field label="Year from"><Input value={ymin} onChange={(e) => setYmin(e.target.value)} inputMode="numeric" /></Field>
              <Field label="Year to"><Input value={ymax} onChange={(e) => setYmax(e.target.value)} inputMode="numeric" /></Field>
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setSmart(false)}>Cancel</Button>
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
              toast.success("Import queued. Matching your library first.");
              setImp(false);
              setUrl("");
              qc.invalidateQueries({ queryKey: ["playlists"] });
            }}
          >
            <Field label="Playlist URL" hint="Spotify, YouTube, SoundCloud, or Apple Music. Creates a SoundDock playlist and matches your library. YouTube fill runs only when ScapeX is enabled.">
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
