import { useEffect, useRef, useState } from "react";
import { Logo } from "@/components/brand/Logo";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { api } from "@/lib/api";
import { DiscordServerButton, HelpButton } from "@/components/community/CommunityLinks";

const errCopy: Record<string, string> = {
  not_in_server: "You must be in the required Discord server to register.",
  missing_role: "You must have the required Discord role to register.",
  oauth_denied: "Discord sign-in was cancelled.",
  disabled: "Discord sign-in is turned off on this instance.",
  token_exchange: "Could not complete Discord sign-in.",
  invalid_state: "Sign-in expired. Try again.",
  account: "Could not create your account.",
  session: "Could not start a session."
};

export function isDiscordOAuthCallbackPath() {
  if (typeof window === "undefined") return false;
  const p = window.location.pathname;
  if (p === "/api/v1/auth/discord/callback") return true;
  if (p === "/api/v1/auth/discord") {
    const q = new URLSearchParams(window.location.search);
    return q.has("code") || q.has("error");
  }
  return false;
}

/** If the PWA served the SPA on the OAuth callback URL, finish login via fetch (not a document navigation). */
export function DiscordCallbackCatch() {
  const [msg, setMsg] = useState("Completing Discord sign-in...");
  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    const target = window.location.pathname + window.location.search;
    (async () => {
      try {
        const res = await fetch(target, { credentials: "include", redirect: "follow" });
        if (res.redirected) {
          const dest = new URL(res.url);
          window.location.replace(dest.pathname + dest.search || "/");
          return;
        }
        const ct = res.headers.get("content-type") || "";
        if (ct.includes("text/html")) {
          const key = "sd_oauth_sw_bypass";
          if (sessionStorage.getItem(key) !== target && "serviceWorker" in navigator) {
            sessionStorage.setItem(key, target);
            const regs = await navigator.serviceWorker.getRegistrations();
            await Promise.all(regs.map((r) => r.unregister()));
            window.location.replace(target);
            return;
          }
          setMsg("Discord sign-in did not complete. Try Continue with Discord again.");
          return;
        }
        window.location.replace("/");
      } catch {
        setMsg("Could not complete Discord sign-in. Try again.");
      }
    })();
  }, []);
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-4">
      <p className="text-sm text-muted">{msg}</p>
    </div>
  );
}

export function LoginPage({
  onDone,
  discordConfigured
}: {
  onDone: () => void;
  discordConfigured?: boolean;
}) {
  const err = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("error") : null;
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [localErr, setLocalErr] = useState("");
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm space-y-4 rounded-2xl border border-border bg-surface-1 p-8 shadow-card">
        <div className="mb-2 text-center">
          <div className="mx-auto mb-4 flex w-max items-center justify-center rounded-xl bg-black p-3">
            <Logo className="h-28 w-auto" />
          </div>
          <h1 className="text-xl font-semibold">Sign in</h1>
          <p className="text-sm text-muted">
            {discordConfigured ? "Use Discord or your local username and password." : "Sign in with your local account. Discord is optional and configured in Admin."}
          </p>
        </div>
        {(err || localErr) && (
          <p className="rounded-lg bg-destructive/15 px-3 py-2 text-sm text-destructive">
            {localErr || errCopy[err || ""] || `Sign-in failed (${err}). Try again.`}
          </p>
        )}
        {discordConfigured && (
          <a
            href="/api/v1/auth/discord"
            className="flex h-10 w-full items-center justify-center rounded-full bg-[#5865F2] text-sm font-semibold text-white hover:opacity-90"
          >
            Continue with Discord
          </a>
        )}
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setLocalErr("");
            try {
              await api.post("/api/v1/auth/login", { username, password });
              onDone();
            } catch {
              setLocalErr("Invalid username or password.");
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label="Username"><Input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required /></Field>
          <Field label="Password"><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required /></Field>
          <Button type="submit" className="w-full" disabled={busy}>{busy ? "Signing in..." : "Sign in"}</Button>
        </form>
        {(err === "not_in_server" || err === "missing_role") && (
          <DiscordServerButton variant="default" size="default" className="w-full" />
        )}
        <div className="flex justify-center gap-2 pt-1">
          <HelpButton />
          <DiscordServerButton variant="ghost" />
        </div>
      </div>
    </div>
  );
}

export function SetupPage({ onDone, discordConfigured }: { onDone: () => void; discordConfigured?: boolean }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [localErr, setLocalErr] = useState("");
  const [mode, setMode] = useState<"create" | "restore">("create");
  const [r2, setR2] = useState({ endpoint: "", bucket: "", access_key: "", secret_key: "", prefix: "", region: "auto" });
  const [remote, setRemote] = useState<{ key: string; name?: string }[]>([]);
  const [passphrase, setPassphrase] = useState("");
  const [reqItems, setReqItems] = useState<{ key: string; class: string; note?: string; recovered?: boolean }[]>([]);
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-4">
      <div className="w-full max-w-md space-y-4 rounded-2xl border border-border bg-surface-1 p-8 shadow-card">
        <div className="mb-2 text-center">
          <div className="mx-auto mb-4 flex w-max items-center justify-center rounded-xl bg-black p-3">
            <Logo className="h-28 w-auto" />
          </div>
          <h1 className="text-xl font-semibold">First setup</h1>
          <p className="text-sm text-muted">
            {mode === "create"
              ? "Create a local administrator. You can enable Discord later under Admin. The first Discord sign-in links to this administrator."
              : "Restore from an encrypted R2 archive. You need the recovery passphrase. This host still needs its own URL and NAS mounts."}
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" size="sm" variant={mode === "create" ? "default" : "secondary"} className="flex-1" onClick={() => setMode("create")}>Create admin</Button>
          <Button type="button" size="sm" variant={mode === "restore" ? "default" : "secondary"} className="flex-1" onClick={() => setMode("restore")}>Restore backup</Button>
        </div>
        {localErr && <p className="rounded-lg bg-destructive/15 px-3 py-2 text-sm text-destructive">{localErr}</p>}
        {reqItems.length > 0 && (
          <div className="rounded-lg border border-border p-3 text-left text-sm">
            <div className="mb-2 font-medium">Restore requirements</div>
            <ul className="space-y-1 text-xs text-muted">
              {reqItems.map((it) => (
                <li key={it.key}>{it.key}: {it.class}{it.note ? ` (${it.note})` : ""}</li>
              ))}
            </ul>
          </div>
        )}
        {mode === "create" && discordConfigured && (
          <a
            href="/api/v1/auth/discord"
            className="flex h-10 w-full items-center justify-center rounded-full bg-[#5865F2] text-sm font-semibold text-white hover:opacity-90"
          >
            Continue with Discord
          </a>
        )}
        {mode === "create" ? (
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setLocalErr("");
            try {
              await api.post("/api/v1/setup", { username, password });
              onDone();
            } catch {
              setLocalErr("Could not create the administrator.");
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label="Username"><Input value={username} onChange={(e) => setUsername(e.target.value)} required /></Field>
          <Field label="Password"><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required /></Field>
          <Button type="submit" className="w-full" disabled={busy}>{busy ? "Creating..." : "Create admin"}</Button>
        </form>
        ) : (
        <div className="space-y-3">
          <Field label="R2 endpoint"><Input value={r2.endpoint} onChange={(e) => setR2({ ...r2, endpoint: e.target.value })} /></Field>
          <Field label="Bucket"><Input value={r2.bucket} onChange={(e) => setR2({ ...r2, bucket: e.target.value })} /></Field>
          <Field label="Access key"><Input value={r2.access_key} onChange={(e) => setR2({ ...r2, access_key: e.target.value })} /></Field>
          <Field label="Secret key"><Input type="password" value={r2.secret_key} onChange={(e) => setR2({ ...r2, secret_key: e.target.value })} /></Field>
          <Field label="Prefix"><Input value={r2.prefix} onChange={(e) => setR2({ ...r2, prefix: e.target.value })} /></Field>
          <Button type="button" className="w-full" disabled={busy} onClick={async () => {
            setBusy(true);
            setLocalErr("");
            try {
              await api.put("/api/v1/setup/backups/settings", { ...r2, r2_enabled: true, use_ssl: true, local_enabled: true, include_media: true });
              const list = await api.get<{ key: string; name?: string }[]>("/api/v1/setup/backups/remote");
              setRemote(list || []);
            } catch (e: any) {
              setLocalErr(e?.message || "Could not list R2 archives.");
            } finally {
              setBusy(false);
            }
          }}>Save and list archives</Button>
          <Field label="Recovery passphrase"><Input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} autoComplete="off" /></Field>
          <ul className="space-y-2">
            {remote.map((o) => (
              <li key={o.key} className="flex items-center justify-between gap-2 text-sm">
                <span className="truncate">{o.name || o.key}</span>
                <Button size="sm" disabled={busy} onClick={async () => {
                  if (!window.confirm("Restore this archive onto this empty host? This wipes the current database.")) return;
                  setBusy(true);
                  setLocalErr("");
                  try {
                    const out = await api.post<{ requirements?: { items?: typeof reqItems } }>("/api/v1/setup/backups/import-remote", {
                      key: o.key, restore: true, confirm: true, passphrase
                    });
                    setReqItems(out?.requirements?.items || []);
                    onDone();
                  } catch (e: any) {
                    setLocalErr(e?.message || "Restore failed.");
                  } finally {
                    setBusy(false);
                  }
                }}>Restore</Button>
              </li>
            ))}
          </ul>
        </div>
        )}
        <div className="flex justify-center gap-2 pt-1">
          <HelpButton />
          <DiscordServerButton variant="ghost" />
        </div>
      </div>
    </div>
  );
}
