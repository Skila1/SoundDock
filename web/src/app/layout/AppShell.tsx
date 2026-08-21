import { useEffect } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { PanelRightOpen } from "lucide-react";
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
import { Button } from "@/components/ui/button";
import { Tooltip } from "@/components/ui/tooltip";
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
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-background">
      <div className="flex min-h-0 flex-1 overflow-hidden">
        <Sidebar user={user} collapsible />
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <TopBar user={user} title={title} />
          <main className="min-h-0 flex-1 overflow-auto px-4 py-5 md:px-8">
            <Outlet />
          </main>
        </div>
        {ui.queueCollapsed ? (
          <aside className="hidden h-full min-h-0 w-12 shrink-0 flex-col items-center border-l border-border bg-surface-1/80 pt-3 xl:flex">
            <Tooltip label="Show queue">
              <Button size="icon" variant="ghost" onClick={() => ui.set({ queueCollapsed: false })} aria-label="Show queue">
                <PanelRightOpen className="h-4 w-4" />
              </Button>
            </Tooltip>
          </aside>
        ) : (
          <aside className="hidden h-full min-h-0 w-[300px] shrink-0 flex-col overflow-hidden border-l border-border bg-surface-1/80 xl:flex">
            <QueuePanel onCollapse={() => ui.set({ queueCollapsed: true })} />
          </aside>
        )}
      </div>
      <PlayerBar />
      <MobileNav />
      <QueueSheet />
      <NowPlaying />
      <CommandSearch />
      <Sheet open={ui.mobileNav} onOpenChange={(v) => ui.set({ mobileNav: v })}>
        <SheetContent side="left" title="Menu">
          <div className="mt-8" onClick={() => ui.set({ mobileNav: false })}>
            <Side user={user} collapsed={false} className="flex w-full border-0 bg-transparent" />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}
