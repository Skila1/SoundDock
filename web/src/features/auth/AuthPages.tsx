import { Logo } from "@/components/brand/Logo";

export function LoginPage(_props: { onDone: () => void }) {
  const err = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("error") : null;
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm space-y-4 rounded-2xl border border-border bg-surface-1 p-8 shadow-card">
        <div className="mb-2 text-center">
          <Logo className="mx-auto mb-4 h-28 w-auto" />
          <h1 className="text-xl font-semibold">Sign in</h1>
          <p className="text-sm text-muted">Accounts are Discord only. The administrator is the Discord user ID in your .env.</p>
        </div>
        {err && <p className="rounded-lg bg-destructive/15 px-3 py-2 text-sm text-destructive">Sign-in failed ({err}). Try again.</p>}
        <a
          href="/api/v1/auth/discord"
          className="flex h-10 w-full items-center justify-center rounded-full bg-[#5865F2] text-sm font-semibold text-white hover:opacity-90"
        >
          Continue with Discord
        </a>
        <p className="text-center text-xs text-subtle">No password. If Discord sign-in is not configured, set SOUNDDOCK_DISCORD_CLIENT_ID in .env.</p>
      </div>
    </div>
  );
}

export function SetupPage({ onDone }: { onDone: () => void }) {
  return <LoginPage onDone={onDone} />;
}
