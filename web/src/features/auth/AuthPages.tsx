import { useEffect, useRef, useState } from "react";
import { Logo } from "@/components/brand/Logo";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { api } from "@/lib/api";

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
      </div>
    </div>
  );
}

export function SetupPage({ onDone, discordConfigured }: { onDone: () => void; discordConfigured?: boolean }) {
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
          <h1 className="text-xl font-semibold">First setup</h1>
          <p className="text-sm text-muted">Create a local administrator. You can enable Discord later under Admin. If Discord is already on, the first Discord sign-in is also an administrator.</p>
        </div>
        {localErr && <p className="rounded-lg bg-destructive/15 px-3 py-2 text-sm text-destructive">{localErr}</p>}
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
      </div>
    </div>
  );
}
