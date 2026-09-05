import { useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Sidebar } from "@/components/navigation/Sidebar";
import { TopBar } from "@/components/navigation/TopBar";
import { MobileNav } from "@/components/navigation/MobileNav";
import { PlayerBar } from "@/components/player/PlayerBar";
import { QueuePanel } from "@/components/player/QueuePanel";
import { QueueSheet } from "@/components/player/QueueSheet";
import { NowPlaying } from "@/components/player/NowPlaying";
import { LyricsPrefetch, LyricsView } from "@/components/player/LyricsView";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { PrefsSync } from "@/stores/prefs";
import { attachAudioListeners, usePlayer } from "@/stores/player";
import { ensureDiscordPresence } from "@/features/settings/discordPresence";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import type { User } from "@/types/api";

const titles: Record<string, string> = {
  "/": "Home",
  "/search": "Search",
  "/library/add": "Add music",
  "/library/import": "Import",
  "/library": "Library",
  "/playlists": "Playlists",
  "/radio": "Radio",
  "/profile/devices": "Devices",
  "/profile/party": "Party",
  "/profile": "Profile",
  "/history": "History",
  "/stats": "Stats",
  "/wrapped": "Wrapped",
  "/settings/connected": "Connected Services",
  "/admin": "Administration"
};

export function AppShell({ user }: { user: User }) {
  const loc = useLocation();
  const ui = useUi();
  useEffect(() => {
    attachAudioListeners();
    usePlayer.getState().load();
    ensureDiscordPresence();
  }, []);
  const title = Object.entries(titles).find(([k]) => (k === "/" ? loc.pathname === "/" : loc.pathname.startsWith(k)))?.[1];
  const queueOpen = ui.queuePinned || !ui.queueCollapsed;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-background">
      <PrefsSync />
      <AnnouncementBanner />
      <div className="flex min-h-0 flex-1 overflow-hidden">
        <Sidebar user={user} collapsible />
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <TopBar user={user} title={title} />
          <main className="min-h-0 flex-1 overflow-auto px-4 py-5 [overflow-anchor:none] md:px-8">
            <Outlet />
          </main>
        </div>
        {queueOpen && (
          <aside className="hidden h-full min-h-0 w-[300px] shrink-0 flex-col overflow-hidden border-l border-border bg-surface-1/80 xl:flex">
            <QueuePanel onCollapse={() => ui.closeQueue()} />
          </aside>
        )}
      </div>
      <PlayerBar />
      <MobileNav />
      <QueueSheet />
      <NowPlaying />
      <LyricsView />
      <LyricsPrefetch />
      <Sheet open={ui.mobileNav} onOpenChange={(v) => ui.set({ mobileNav: v })}>
        <SheetContent side="left" title="Menu">
          <div className="mt-8" onClick={() => ui.set({ mobileNav: false })}>
            <Sidebar user={user} collapsed={false} className="flex w-full border-0 bg-transparent" />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}

function AnnouncementBanner() {
  const q = useQuery({
    queryKey: ["announcement"],
    queryFn: () => api.get<{ enabled?: boolean; message?: string }>("/api/v1/announcement"),
    refetchInterval: 60_000
  });
  const [dismissed, setDismissed] = useState("");
  const msg = q.data?.enabled ? (q.data.message || "") : "";
  if (!msg || dismissed === msg) return null;
  return (
    <div className="flex items-center justify-between gap-3 border-b border-border bg-accent/15 px-4 py-2 text-sm">
      <span>{msg}</span>
      <Button size="sm" variant="ghost" onClick={() => setDismissed(msg)}>
        Dismiss
      </Button>
    </div>
  );
}
