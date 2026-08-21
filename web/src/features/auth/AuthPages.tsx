import { useState } from "react";
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
          <Logo className="mx-auto mb-4 h-28 w-auto" />
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
          <Logo className="mx-auto mb-4 h-28 w-auto" />
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
