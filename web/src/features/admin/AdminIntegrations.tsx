import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { relativeTime } from "@/lib/utils";
import { AdminTabs, Card, SettingSwitch, errorMessage } from "./adminUi";

export function AdminIntegrations() {
  return (
    <div>
      <PageHeader title="Integrations" description="API keys, outgoing webhooks, and the playlist services people can connect." />
      <AdminTabs
        fallback="keys"
        tabs={[
          { id: "keys", label: "API keys", content: <ApiKeys /> },
          { id: "webhooks", label: "Webhooks", content: <Webhooks /> },
          { id: "providers", label: "Playlist providers", content: <PlaylistProviders /> }
        ]}
      />
    </div>
  );
}

const apiKeyScopes = [
  { name: "admin", label: "Administrator", desc: "Everything, including logs, users, Discord, and updates" },
  { name: "tracks.read", label: "Read catalogue", desc: "List tracks, albums, artists, and search" },
  { name: "tracks.stream", label: "Stream audio", desc: "Play files from the library" },
  { name: "playlists.write", label: "Playlists", desc: "Create and edit playlists" },
  { name: "history.read", label: "History", desc: "Listening history and stats" },
  { name: "library.upload", label: "Upload", desc: "Upload into writable libraries" },
  { name: "library.import_url", label: "Import from URL", desc: "Import remote files" },
  { name: "library.create", label: "Create libraries", desc: "Add library folders" },
  { name: "library.migrate", label: "Move libraries", desc: "Move files into managed storage" }
];

const scopePresets: Record<string, string[]> = {
  listen: ["tracks.read", "tracks.stream", "history.read"],
  library: ["tracks.read", "tracks.stream", "playlists.write", "history.read", "library.upload"],
  admin: ["admin"]
};

const presetOptions = [
  { value: "listen", label: "Listen (read and stream)" },
  { value: "library", label: "Library (listen, playlists, upload)" },
  { value: "admin", label: "Administrator" },
  { value: "custom", label: "Custom…" }
];

function ApiKeys() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-int"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations") });
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [preset, setPreset] = useState("listen");
  const [custom, setCustom] = useState<string[]>([]);
  const [secret, setSecret] = useState("");
  const [revoke, setRevoke] = useState<any | null>(null);
  const scopes = preset === "custom" ? custom : scopePresets[preset];

  async function create(e: React.FormEvent) {
    e.preventDefault();
    if (!scopes.length) {
      toast.error("Pick at least one permission");
      return;
    }
    try {
      const r = await api.post<any>("/api/v1/admin/integrations", { name, scopes });
      setSecret(r.secret);
      setName("");
      setCreating(false);
      qc.invalidateQueries({ queryKey: ["admin-int"] });
    } catch (err) {
      toast.error(errorMessage(err, "Could not create key"));
    }
  }

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted">Keys let scripts and other apps use SoundDock. The key is shown once when it is created.</p>
        <Button onClick={() => { setCreating(true); setPreset("listen"); setCustom([]); }}>Create key</Button>
      </div>
      {secret && (
        <div className="mb-4 rounded-xl border border-accent/40 bg-accent/10 p-4 text-sm">
          <p className="mb-2 font-medium">Copy this key now. It will not be shown again.</p>
          <div className="flex flex-wrap items-center gap-2">
            <code className="min-w-0 flex-1 break-all rounded-md bg-surface-2 px-2 py-1">{secret}</code>
            <Button size="sm" variant="secondary" onClick={() => { void navigator.clipboard?.writeText(secret); toast.success("Copied"); }}><Copy /> Copy</Button>
            <Button size="sm" variant="ghost" onClick={() => setSecret("")}>Done</Button>
          </div>
        </div>
      )}
      <ul className="divide-y divide-border rounded-xl border border-border">
        {(q.data || []).map((c) => (
          <li key={c.id} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0">
              <div className="font-medium">{c.name}</div>
              <div className="truncate text-xs text-muted">
                {c.prefix ? `${c.prefix}… · ` : ""}{(c.scopes || []).join(", ") || "no permissions"}
                {c.last_used_at ? ` · used ${relativeTime(c.last_used_at)}` : " · never used"}
              </div>
            </div>
            <Button size="sm" variant="ghost" onClick={() => setRevoke(c)}>Revoke</Button>
          </li>
        ))}
        {!q.data?.length && <li className="px-4 py-6 text-center text-sm text-muted">{q.isLoading ? "Loading…" : "No API keys yet."}</li>}
      </ul>

      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent title="Create API key" className="max-h-[90vh] overflow-y-auto">
          <form className="space-y-4" onSubmit={create}>
            <Field label="Name" hint="Something that tells you where the key is used.">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="home-assistant" required />
            </Field>
            <Field label="Permissions">
              <Select value={preset} onValueChange={(v) => { setPreset(v); if (v === "custom" && !custom.length) setCustom(scopePresets.listen); }} options={presetOptions} />
            </Field>
            {preset === "custom" && (
              <div className="grid gap-2 sm:grid-cols-2">
                {apiKeyScopes.map((s) => (
                  <label key={s.name} className="flex items-start gap-3 rounded-lg border border-border px-3 py-2">
                    <input type="checkbox" className="mt-1" checked={custom.includes(s.name)} onChange={() => setCustom((cur) => (cur.includes(s.name) ? cur.filter((x) => x !== s.name) : [...cur, s.name]))} />
                    <span>
                      <span className="block text-sm font-medium">{s.label}</span>
                      <span className="block text-xs text-muted">{s.desc}</span>
                    </span>
                  </label>
                ))}
              </div>
            )}
            <Button type="submit">Create key</Button>
          </form>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={!!revoke}
        onOpenChange={(v) => { if (!v) setRevoke(null); }}
        title={`Revoke ${revoke?.name || "key"}?`}
        description="Anything using this key stops working immediately."
        confirmLabel="Revoke"
        destructive
        onConfirm={async () => {
          try {
            await api.del(`/api/v1/admin/integrations/${revoke.id}`);
            toast.success("Key revoked");
            qc.invalidateQueries({ queryKey: ["admin-int"] });
          } catch (err) {
            toast.error(errorMessage(err, "Could not revoke key"));
          }
        }}
      />
    </div>
  );
}

const webhookEvents = [
  { name: "playback.started", label: "Playback started" },
  { name: "playback.finished", label: "Playback stopped" },
  { name: "library.scan.completed", label: "Library scan finished" },
  { name: "track.added", label: "Track added" },
  { name: "playlist.created", label: "Playlist created" },
  { name: "external.playlist.imported", label: "Playlist import queued" },
  { name: "external.playlist.sync.completed", label: "Playlist sync finished" },
  { name: "external.provider.connected", label: "Provider connected" },
  { name: "external.provider.disconnected", label: "Provider disconnected" }
];

function Webhooks() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["webhooks"], queryFn: () => api.get<any[]>("/api/v1/admin/webhooks") });
  const [adding, setAdding] = useState(false);
  const [url, setUrl] = useState("");
  const [secret, setSecret] = useState("");
  const [events, setEvents] = useState<string[]>(["playback.started", "library.scan.completed"]);
  const [remove, setRemove] = useState<any | null>(null);
  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted">SoundDock posts a signed JSON message to each URL when the chosen events happen. Failed deliveries appear under Activity.</p>
        <Button onClick={() => setAdding(true)}>Add webhook</Button>
      </div>
      <ul className="divide-y divide-border rounded-xl border border-border">
        {(q.data || []).map((w) => (
          <li key={w.id} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{w.url}</div>
              <div className="truncate text-xs text-muted">{(w.events || []).join(", ")}</div>
            </div>
            <div className="flex items-center gap-2">
              {w.enabled === false && <Badge tone="warning">Off</Badge>}
              <Button size="sm" variant="ghost" aria-label="Delete webhook" onClick={() => setRemove(w)}><Trash2 /></Button>
            </div>
          </li>
        ))}
        {!q.data?.length && <li className="px-4 py-6 text-center text-sm text-muted">{q.isLoading ? "Loading…" : "No webhooks yet."}</li>}
      </ul>
      <Dialog open={adding} onOpenChange={setAdding}>
        <DialogContent title="Add webhook">
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              if (!events.length) {
                toast.error("Pick at least one event");
                return;
              }
              try {
                await api.post("/api/v1/admin/webhooks", { url, secret, events });
                toast.success("Webhook added");
                setUrl("");
                setSecret("");
                setAdding(false);
                qc.invalidateQueries({ queryKey: ["webhooks"] });
              } catch (err) {
                toast.error(errorMessage(err, "Could not add webhook"));
              }
            }}
          >
            <Field label="URL"><Input type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/hooks/sounddock" required /></Field>
            <Field label="Signing secret" hint="Used to sign each message (X-SoundDock-Signature). Stored encrypted."><Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} /></Field>
            <div>
              <div className="mb-2 text-sm font-medium">Events</div>
              <div className="grid gap-1 sm:grid-cols-2">
                {webhookEvents.map((ev) => (
                  <label key={ev.name} className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={events.includes(ev.name)} onChange={() => setEvents((cur) => (cur.includes(ev.name) ? cur.filter((x) => x !== ev.name) : [...cur, ev.name]))} />
                    {ev.label}
                  </label>
                ))}
              </div>
            </div>
            <Button type="submit">Add webhook</Button>
          </form>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={!!remove}
        onOpenChange={(v) => { if (!v) setRemove(null); }}
        title="Delete webhook?"
        description={remove?.url || ""}
        confirmLabel="Delete"
        destructive
        onConfirm={async () => {
          try {
            await api.del(`/api/v1/admin/webhooks/${remove.id}`);
            qc.invalidateQueries({ queryKey: ["webhooks"] });
          } catch (err) {
            toast.error(errorMessage(err, "Could not delete webhook"));
          }
        }}
      />
    </div>
  );
}

const providerNames: Record<string, string> = { spotify: "Spotify", youtube: "YouTube", soundcloud: "SoundCloud", apple_music: "Apple Music" };

function PlaylistProviders() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["ext-providers"], queryFn: () => api.get<any[]>("/api/v1/admin/integrations/external-providers") });
  return (
    <div>
      <p className="mb-3 text-sm text-muted">Let people import playlists from other services. Imported playlists match your library first, then download anything missing.</p>
      <div className="grid gap-4 lg:grid-cols-2">
        {(q.data || []).map((p) => (
          <ProviderCard key={p.provider} p={p} onSaved={() => qc.invalidateQueries({ queryKey: ["ext-providers"] })} />
        ))}
      </div>
    </div>
  );
}

function ProviderCard({ p, onSaved }: { p: any; onSaved: () => void }) {
  const [enabled, setEnabled] = useState(!!p.enabled);
  const [may, setMay] = useState(!!p.users_may_connect);
  const [pub, setPub] = useState(!!p.public_import);
  const [cid, setCid] = useState(p.client_id || "");
  const [secret, setSecret] = useState("");
  const [extra, setExtra] = useState("");
  const [saving, setSaving] = useState(false);
  const dirty = enabled !== !!p.enabled || may !== !!p.users_may_connect || pub !== !!p.public_import || cid !== (p.client_id || "") || !!secret || !!extra;
  const caps = p.capabilities
    ? [p.capabilities.list_user_playlists && "user playlists", p.capabilities.private_playlists && "private playlists", p.capabilities.isrc && "ISRC matching", p.capabilities.snapshot && "change tracking"].filter(Boolean).join(" · ")
    : "";
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault();
        setSaving(true);
        const body: any = { enabled, users_may_connect: may, public_import: pub, client_id: cid };
        if (secret) body.client_secret = secret;
        if (extra) body.extra = p.provider === "apple_music" ? { developer_token: extra } : { api_key: extra };
        try {
          await api.put(`/api/v1/admin/integrations/external-providers/${p.provider}`, body);
          toast.success(`${providerNames[p.provider] || p.provider} saved`);
          setSecret("");
          setExtra("");
          onSaved();
        } catch (err) {
          toast.error(errorMessage(err, "Could not save provider"));
        } finally {
          setSaving(false);
        }
      }}
    >
      <Card
        title={providerNames[p.provider] || p.provider}
        description={caps || undefined}
        actions={p.has_client_secret ? <Badge tone="success">Credentials saved</Badge> : <Badge>Not set up</Badge>}
      >
        <div className="divide-y divide-border">
          <SettingSwitch label="Enabled" checked={enabled} onChange={setEnabled} />
          <SettingSwitch label="People can connect their account" checked={may} onChange={setMay} />
          <SettingSwitch label="Import public playlists by link" checked={pub} onChange={setPub} />
        </div>
        <div className="mt-3 space-y-3">
          {p.provider !== "apple_music" && (
            <>
              <Field label="Client ID"><Input value={cid} onChange={(e) => setCid(e.target.value)} /></Field>
              <Field label="Client secret" hint={p.has_client_secret ? "Leave blank to keep the saved secret." : undefined}><Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} /></Field>
            </>
          )}
          {p.provider === "youtube" && (
            <Field label="API key (optional)" hint="Only needed to read public playlists without signing in."><Input type="password" value={extra} onChange={(e) => setExtra(e.target.value)} /></Field>
          )}
          {p.provider === "apple_music" && (
            <Field label="MusicKit developer token" hint="Stored encrypted."><Input type="password" value={extra} onChange={(e) => setExtra(e.target.value)} /></Field>
          )}
          {p.callback_url && <p className="break-all text-xs text-subtle">Redirect URL to register with {providerNames[p.provider] || p.provider}: <code>{p.callback_url}</code></p>}
          <Button type="submit" size="sm" disabled={!dirty || saving}>{saving ? "Saving…" : "Save"}</Button>
        </div>
      </Card>
    </form>
  );
}
