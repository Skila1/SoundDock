import { useEffect } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { Sidebar } from "@/components/navigation/Sidebar";
import { TopBar } from "@/components/navigation/TopBar";
import { MobileNav } from "@/components/navigation/MobileNav";
import { CommandSearch } from "@/components/navigation/CommandSearch";
import { PlayerBar } from "@/components/player/PlayerBar";
import { QueuePanel } from "@/components/player/QueuePanel";
import { QueueSheet } from "@/components/player/QueueSheet";
import { NowPlaying } from "@/components/player/NowPlaying";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { Sidebar as Side } from "@/components/navigation/Sidebar";
import { attachAudioListeners, usePlayer } from "@/stores/player";
import { useUi } from "@/stores/ui";
import type { User } from "@/types/api";

const titles: Record<string, string> = {
  "/": "Home",
  "/search": "Search",
  "/artists": "Artists",
  "/albums": "Albums",
  "/tracks": "Tracks",
  "/playlists": "Playlists",
  "/favourites": "Favourites",
  "/library": "Libraries",
  "/upload": "Upload",
  "/import": "Remote Import",
  "/profile": "Profile",
  "/admin": "Administration"
};

export function AppShell({ user }: { user: User }) {
  const loc = useLocation();
  const ui = useUi();
  useEffect(() => {
    attachAudioListeners();
    usePlayer.getState().load();
  }, []);
  const title = Object.entries(titles).find(([k]) => (k === "/" ? loc.pathname === "/" : loc.pathname.startsWith(k)))?.[1];

  return (
    <div className="flex h-dvh flex-col bg-background">
      <div className="flex min-h-0 flex-1">
        <Sidebar user={user} />
        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar user={user} title={title} />
          <main className="min-h-0 flex-1 overflow-auto px-4 py-5 md:px-8">
            <Outlet />
          </main>
        </div>
        <aside className="hidden h-full w-[300px] shrink-0 flex-col border-l border-border bg-surface-1/80 xl:flex">
          <QueuePanel />
        </aside>
      </div>
      <PlayerBar />
      <MobileNav />
      <QueueSheet />
      <NowPlaying />
      <CommandSearch />
      <Sheet open={ui.mobileNav} onOpenChange={(v) => ui.set({ mobileNav: v })}>
        <SheetContent side="left" title="Menu">
          <div className="mt-8" onClick={() => ui.set({ mobileNav: false })}>
            <Side user={user} className="flex w-full border-0 bg-transparent" />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}
