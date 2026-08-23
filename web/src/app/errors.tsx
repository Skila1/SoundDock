import { isRouteErrorResponse, useNavigate, useRouteError } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Logo } from "@/components/brand/Logo";
import { DiscordServerButton, HelpButton } from "@/components/community/CommunityLinks";

export function NotFoundPage() {
  const nav = useNavigate();
  return (
    <div className="py-20 text-center">
      <h1 className="text-3xl font-semibold">Page not found</h1>
      <p className="mt-2 text-muted">That route doesn’t exist in SoundDock.</p>
      <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
        <Button onClick={() => nav("/")}>Go home</Button>
        <HelpButton variant="secondary" size="default" />
        <DiscordServerButton variant="outline" size="default" />
      </div>
    </div>
  );
}

export function ForbiddenPage() {
  return (
    <div className="py-20 text-center">
      <h1 className="text-3xl font-semibold">Permission denied</h1>
      <p className="mt-2 text-muted">You don’t have access to this area.</p>
      <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
        <HelpButton variant="secondary" size="default" />
        <DiscordServerButton variant="outline" size="default" />
      </div>
    </div>
  );
}

export function RouteError() {
  const err = useRouteError();
  const msg = isRouteErrorResponse(err) ? err.statusText : err instanceof Error ? err.message : "Something went wrong";
  return (
    <div className="py-20 text-center">
      <h1 className="text-3xl font-semibold">Couldn’t load this page</h1>
      <p className="mt-2 text-muted">{msg}</p>
      <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
        <Button onClick={() => location.reload()}>Retry</Button>
        <HelpButton variant="secondary" size="default" />
        <DiscordServerButton variant="outline" size="default" />
      </div>
    </div>
  );
}

export function BootScreen() {
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center bg-background">
      <div className="mb-4 rounded-xl bg-black p-3">
        <Logo className="h-24 w-auto" />
      </div>
      <div className="h-1 w-40 overflow-hidden rounded-full bg-surface-2">
        <div className="h-full w-1/2 animate-pulse bg-accent" />
      </div>
      <p className="mt-4 text-sm text-muted">Starting SoundDock</p>
    </div>
  );
}
