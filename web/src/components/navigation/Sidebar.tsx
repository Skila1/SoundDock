import { NavLink } from "react-router-dom";
import {
  Disc3,
  Heart,
  Home,
  Library,
  Link2,
  ListMusic,
  Mic2,
  PanelLeftClose,
  PanelLeftOpen,
  Radio,
  Search,
  Settings,
  Upload,
  Globe,
  Music
} from "lucide-react";
import { Logo } from "@/components/brand/Logo";
import { Button } from "@/components/ui/button";
import { Tooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useUi } from "@/stores/ui";
import type { User } from "@/types/api";

const items = [
  { to: "/", label: "Home", icon: Home, end: true },
  { to: "/search", label: "Search", icon: Search },
  { to: "/artists", label: "Artists", icon: Mic2 },
  { to: "/albums", label: "Albums", icon: Disc3 },
  { to: "/tracks", label: "Tracks", icon: Music },
  { to: "/playlists", label: "Playlists", icon: ListMusic },
  { to: "/radio", label: "Radio", icon: Radio },
  { to: "/settings/connected", label: "Connected Services", icon: Link2 },
  { to: "/favourites", label: "Favourites", icon: Heart },
  { to: "/library", label: "Libraries", icon: Library },
  { to: "/upload", label: "Upload", icon: Upload },
  { to: "/import", label: "Remote Import", icon: Globe }
];

const profileItems = [
  { to: "/history", label: "History" },
  { to: "/stats", label: "Listening stats" },
  { to: "/wrapped", label: "Wrapped" },
  { to: "/profile/devices", label: "Devices" },
  { to: "/profile/party", label: "Party" }
];

export function Sidebar({ user, collapsed, className, collapsible = false }: { user: User; collapsed?: boolean; className?: string; collapsible?: boolean }) {
  const ui = useUi();
  const compact = collapsed ?? ui.navCollapsed;
  return (
    <aside className={cn("h-full min-h-0 flex-col overflow-hidden border-r border-border bg-surface-1/80", className || "hidden md:flex", compact ? "w-[72px]" : "w-[232px]")}>
      <div className="flex items-center bg-black">
        <NavLink to="/" end className={cn("flex min-w-0 flex-1 items-center px-3 py-4", compact && "justify-center px-2")}>
          <Logo className={compact ? "h-9 w-9" : "h-12 w-auto max-w-[168px]"} />
        </NavLink>
        {collapsible && !compact && (
          <Tooltip label="Collapse menu">
            <Button size="icon" variant="ghost" className="mr-2 h-8 w-8 text-white hover:bg-white/10" onClick={() => ui.set({ navCollapsed: true })} aria-label="Collapse menu">
              <PanelLeftClose className="h-4 w-4" />
            </Button>
          </Tooltip>
        )}
      </div>
      <nav className="flex-1 space-y-0.5 px-2 pt-2">
        {items.map((it) => (
          <NavLink
            key={it.to}
            to={it.to}
            end={it.end}
            title={compact ? it.label : undefined}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-foreground",
                compact && "justify-center px-0",
                isActive && "bg-surface-2 text-foreground"
              )
            }
          >
            <it.icon className="h-4 w-4 shrink-0" />
            {!compact && it.label}
          </NavLink>
        ))}
        {user.is_admin && (
          <NavLink to="/admin" title={compact ? "Administration" : undefined} className={({ isActive }) => cn("mt-4 flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted hover:bg-surface-2", compact && "justify-center px-0", isActive && "bg-surface-2 text-foreground")}>
            <Settings className="h-4 w-4" />
            {!compact && "Administration"}
          </NavLink>
        )}
      </nav>
      {!compact && (
        <div className="space-y-0.5 px-2 pb-2">
          {profileItems.map((it) => (
            <NavLink
              key={it.to}
              to={it.to}
              className={({ isActive }) =>
                cn("block rounded-lg px-3 py-1.5 text-xs text-muted hover:bg-surface-2 hover:text-foreground", isActive && "bg-surface-2 text-foreground")
              }
            >
              {it.label}
            </NavLink>
          ))}
        </div>
      )}
      {collapsible && compact && (
        <div className="px-2 pb-2">
          <Tooltip label="Expand menu">
            <Button size="icon" variant="ghost" className="w-full" onClick={() => ui.set({ navCollapsed: false })} aria-label="Expand menu">
              <PanelLeftOpen className="h-4 w-4" />
            </Button>
          </Tooltip>
        </div>
      )}
      <NavLink to="/profile" className={cn("m-3 flex items-center gap-3 rounded-lg bg-surface-2 px-3 py-2", compact && "justify-center px-2")}>
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-accent/20 text-xs font-bold text-accent">
          {(user.display_name || user.username).slice(0, 1).toUpperCase()}
        </div>
        {!compact && (
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{user.display_name || user.username}</div>
            <div className="truncate text-xs text-subtle">{user.is_admin ? "Administrator" : "Listener"}</div>
          </div>
        )}
      </NavLink>
    </aside>
  );
}
