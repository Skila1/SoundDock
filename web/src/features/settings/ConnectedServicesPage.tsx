import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Link2 } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";

const labels: Record<string, string> = {
  spotify: "Spotify",
  youtube: "YouTube",
  soundcloud: "SoundCloud",
  apple_music: "Apple Music"
};

export function ConnectedServicesPage() {
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const q = useQuery({ queryKey: ["me-providers"], queryFn: () => api.get<any[]>("/api/v1/me/providers") });
  const [appleTok, setAppleTok] = useState("");

  useEffect(() => {
    if (params.get("connected")) toast.success(`Connected ${labels[params.get("connected")!] || params.get("connected")}`);
    if (params.get("error")) toast.error("Could not connect provider");
  }, [params]);

  return (
    <div className="max-w-2xl">
      <PageHeader
        title="Connected Services"
        description="Link playlist providers to import and keep playlists in sync. SoundDock never downloads their audio."
      />
      <div className="space-y-3">
        {(q.data || []).map((p) => (
          <article key={p.provider} className="rounded-xl border border-border bg-surface-1 p-4">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <Link2 className="h-4 w-4 text-accent" />
                  <h2 className="font-semibold">{labels[p.provider] || p.provider}</h2>
                  {p.connected ? <Badge tone="success">Connected</Badge> : <Badge>Not connected</Badge>}
                </div>
                {p.account_name && <p className="mt-1 text-sm text-muted">{p.account_name}</p>}
                {p.scopes?.length ? <p className="text-xs text-subtle">{p.scopes.join(", ")}</p> : null}
                {p.last_error ? <p className="text-xs text-destructive">{p.last_error}</p> : null}
              </div>
              <div className="flex gap-2">
                {p.provider === "apple_music" && p.enabled && p.users_may_connect && !p.connected && null}
                {p.enabled && p.users_may_connect && p.provider !== "apple_music" && (
                  <Button
                    size="sm"
                    onClick={async () => {
                      const r = await api.post<{ url: string }>(`/api/v1/me/providers/${p.provider}/connect`);
                      window.location.href = r.url;
                    }}
                  >
                    {p.connected ? "Reconnect" : "Connect"}
                  </Button>
                )}
                {p.connected && (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={async () => {
                      await api.del(`/api/v1/me/providers/${p.provider}`);
                      toast.success("Disconnected. Playlists kept, media untouched");
                      qc.invalidateQueries({ queryKey: ["me-providers"] });
                    }}
                  >
                    Disconnect
                  </Button>
                )}
              </div>
            </div>
            {p.provider === "apple_music" && p.enabled && p.users_may_connect && (
              <form
                className="mt-3 space-y-2"
                onSubmit={async (e) => {
                  e.preventDefault();
                  await api.post("/api/v1/me/providers/apple_music/connect", { music_user_token: appleTok, name: "Apple Music" });
                  toast.success("Apple Music connected");
                  setAppleTok("");
                  qc.invalidateQueries({ queryKey: ["me-providers"] });
                }}
              >
                <Field label="Music User Token" hint="From MusicKit JS after you authorize this instance. Stored encrypted on the server, never in the browser.">
                  <Textarea value={appleTok} onChange={(e) => setAppleTok(e.target.value)} rows={3} />
                </Field>
                <Button type="submit" size="sm">Save token</Button>
              </form>
            )}
            {!p.enabled && <p className="mt-2 text-xs text-subtle">An administrator must enable this provider.</p>}
          </article>
        ))}
      </div>
    </div>
  );
}
