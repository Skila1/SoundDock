import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { relativeTime } from "@/lib/utils";
import { toast } from "sonner";
import type { User } from "@/types/api";

type SessionRow = {
  id: string;
  user_agent?: string | null;
  ip?: string | null;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
};

type TokenRow = {
  id: string;
  name: string;
  prefix: string;
  scopes?: string[];
  last_used_at?: string | null;
  created_at: string;
};

export function ProfilePage({ user, onRefresh }: { user: User; onRefresh: () => void }) {
  const qc = useQueryClient();
  const [display, setDisplay] = useState(user.display_name || "");
  const [rg, setRg] = useState(user.replaygain_mode || "off");
  const [xf, setXf] = useState(String(user.crossfade_seconds || 0));
  const [lufs, setLufs] = useState(String(user.target_lufs ?? -18));
  const [tokenName, setTokenName] = useState("");
  const [freshSecret, setFreshSecret] = useState("");

  const sessions = useQuery({
    queryKey: ["me-sessions"],
    queryFn: () => api.get<SessionRow[] | null>("/api/v1/me/sessions")
  });
  const tokens = useQuery({
    queryKey: ["me-tokens"],
    queryFn: () => api.get<TokenRow[] | null>("/api/v1/me/tokens")
  });

  const sessionRows = Array.isArray(sessions.data) ? sessions.data : [];
  const tokenRows = Array.isArray(tokens.data) ? tokens.data : [];

  return (
    <div className="max-w-2xl">
      <PageHeader title="Profile" description="Account, playback quality, sessions, and API tokens." />
      <form
        className="space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.patch("/api/v1/me", {
              display_name: display,
              replaygain_mode: rg,
              crossfade_seconds: Number(xf),
              target_lufs: Number(lufs)
            });
            toast.success("Settings saved");
            onRefresh();
          } catch (err) {
            toast.error(err instanceof Error ? err.message : "Could not save");
          }
        }}
      >
        <Field label="Display name"><Input value={display} onChange={(e) => setDisplay(e.target.value)} /></Field>
        <Field label="ReplayGain">
          <Select value={rg} onValueChange={setRg} options={[{ value: "off", label: "Off" }, { value: "track", label: "Track" }, { value: "album", label: "Album" }]} />
        </Field>
        <Field label="Crossfade (seconds)"><Input type="number" min={0} max={12} value={xf} onChange={(e) => setXf(e.target.value)} /></Field>
        <Field label="Target loudness (LUFS)" hint="Quality preference on your account. Typical album target is −18.">
          <Input type="number" min={-30} max={-6} step={0.5} value={lufs} onChange={(e) => setLufs(e.target.value)} />
        </Field>
        <Button type="submit">Save</Button>
      </form>
      <form
        className="mt-6 space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          const fd = new FormData(e.currentTarget);
          try {
            await api.post("/api/v1/me/password", { current: fd.get("current"), new: fd.get("next") });
            toast.success("Password updated");
            e.currentTarget.reset();
          } catch (err) {
            toast.error(err instanceof Error ? err.message : "Could not change password");
          }
        }}
      >
        <h2 className="font-semibold">Password</h2>
        <Field label="Current"><Input name="current" type="password" autoComplete="current-password" required /></Field>
        <Field label="New"><Input name="next" type="password" autoComplete="new-password" required /></Field>
        <Button type="submit">Change password</Button>
      </form>
      <section className="mt-6 space-y-3 rounded-xl border border-border bg-surface-1 p-5">
        <h2 className="font-semibold">Sessions</h2>
        <p className="text-sm text-muted">Signed-in browsers and apps. Revoke one without logging everyone out.</p>
        {sessionRows.length === 0 && !sessions.isLoading && <p className="text-sm text-subtle">No active sessions.</p>}
        <ul className="space-y-2">
          {sessionRows.map((s) => (
            <li key={s.id} className="flex items-start justify-between gap-3 rounded-lg border border-border px-3 py-2">
              <div className="min-w-0">
                <div className="truncate text-sm font-medium">{s.user_agent || "Unknown client"}</div>
                <div className="text-xs text-muted">
                  {s.ip || "no IP"} · last seen {relativeTime(s.last_seen_at)}
                </div>
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={async () => {
                  try {
                    await api.del(`/api/v1/me/sessions/${s.id}`);
                    toast.success("Session revoked");
                    qc.invalidateQueries({ queryKey: ["me-sessions"] });
                  } catch (err) {
                    toast.error(err instanceof Error ? err.message : "Could not revoke");
                  }
                }}
              >
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      </section>
      <section className="mt-6 space-y-3 rounded-xl border border-border bg-surface-1 p-5">
        <h2 className="font-semibold">Personal access tokens</h2>
        <p className="text-sm text-muted">For scripts and automations. The secret is shown once.</p>
        <form
          className="flex max-w-lg gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              const r = await api.post<{ secret: string }>("/api/v1/me/tokens", { name: tokenName, scopes: [] });
              setFreshSecret(r.secret);
              setTokenName("");
              toast.success("Token created");
              qc.invalidateQueries({ queryKey: ["me-tokens"] });
            } catch (err) {
              toast.error(err instanceof Error ? err.message : "Could not create token");
            }
          }}
        >
          <Input value={tokenName} onChange={(e) => setTokenName(e.target.value)} placeholder="Token name" required />
          <Button type="submit">Create</Button>
        </form>
        {freshSecret && (
          <p className="rounded-lg bg-surface-2 p-3 text-sm">
            Copy this key now: <code className="break-all">{freshSecret}</code>
          </p>
        )}
        <ul className="space-y-2">
          {tokenRows.map((t) => (
            <li key={t.id} className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2">
              <div>
                <div className="font-medium">{t.name}</div>
                <div className="text-xs text-muted">
                  {t.prefix}… · created {relativeTime(t.created_at)}
                  {t.last_used_at ? ` · used ${relativeTime(t.last_used_at)}` : " · never used"}
                </div>
                {(t.scopes || []).length > 0 && <Badge className="mt-1">{(t.scopes || []).join(", ")}</Badge>}
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={async () => {
                  try {
                    await api.del(`/api/v1/me/tokens/${t.id}`);
                    toast.success("Token revoked");
                    qc.invalidateQueries({ queryKey: ["me-tokens"] });
                  } catch (err) {
                    toast.error(err instanceof Error ? err.message : "Could not revoke");
                  }
                }}
              >
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      </section>
      <div className="mt-6 flex flex-wrap gap-2">
        <Button variant="outline" onClick={() => (window.location.href = "/settings/connected")}>Connected services</Button>
        <Button variant="outline" onClick={() => (window.location.href = "/profile/devices")}>Devices</Button>
        <Button variant="outline" onClick={() => (window.location.href = "/profile/party")}>Party</Button>
        <Button variant="outline" onClick={() => window.open("/api/v1/me/export")}>Export my data</Button>
        <Button variant="ghost" onClick={() => api.post("/api/v1/auth/logout-all").then(() => toast.success("Sessions revoked"))}>Revoke other sessions</Button>
      </div>
    </div>
  );
}
