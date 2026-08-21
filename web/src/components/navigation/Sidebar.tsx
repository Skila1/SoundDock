import { NavLink } from "react-router-dom";
import {
  Disc3,
  Heart,
  Home,
  Library,
  Link2,
  ListMusic,
  Mic2,
  Search,
  Settings,
  Upload,
  Globe,
  Music
} from "lucide-react";
import { Logo } from "@/components/brand/Logo";
import { cn } from "@/lib/utils";
import type { User } from "@/types/api";

const items = [
  { to: "/", label: "Home", icon: Home, end: true },
  { to: "/search", label: "Search", icon: Search },
  { to: "/artists", label: "Artists", icon: Mic2 },
  { to: "/albums", label: "Albums", icon: Disc3 },
  { to: "/tracks", label: "Tracks", icon: Music },
  { to: "/playlists", label: "Playlists", icon: ListMusic },
  { to: "/settings/connected", label: "Connected Services", icon: Link2 },
  { to: "/favourites", label: "Favourites", icon: Heart },
  { to: "/library", label: "Libraries", icon: Library },
  { to: "/upload", label: "Upload", icon: Upload },
  { to: "/import", label: "Remote Import", icon: Globe }
];

export function Sidebar({ user, collapsed, className }: { user: User; collapsed?: boolean; className?: string }) {
  return (
    <aside className={cn("h-full flex-col border-r border-border bg-surface-1/80", className || "hidden md:flex", collapsed ? "w-[72px]" : "w-[232px]")}>
      <NavLink to="/" end className="flex items-center px-3 py-4">
        <Logo className={collapsed ? "h-9 w-9" : "h-12 w-auto max-w-[196px]"} />
      </NavLink>
      <nav className="flex-1 space-y-0.5 px-2">
        {items.map((it) => (
          <NavLink
            key={it.to}
            to={it.to}
            end={it.end}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-foreground",
                isActive && "bg-surface-2 text-foreground before:absolute"
              )
            }
          >
            <it.icon className="h-4 w-4 shrink-0" />
            {!collapsed && it.label}
          </NavLink>
        ))}
        {user.is_admin && (
          <NavLink to="/admin" className={({ isActive }) => cn("mt-4 flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted hover:bg-surface-2", isActive && "bg-surface-2 text-foreground")}>
            <Settings className="h-4 w-4" />
            {!collapsed && "Administration"}
          </NavLink>
        )}
      </nav>
      <NavLink to="/profile" className="m-3 flex items-center gap-3 rounded-lg bg-surface-2 px-3 py-2">
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-accent/20 text-xs font-bold text-accent">
          {(user.display_name || user.username).slice(0, 1).toUpperCase()}
        </div>
        {!collapsed && (
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{user.display_name || user.username}</div>
            <div className="truncate text-xs text-subtle">{user.is_admin ? "Administrator" : "Listener"}</div>
          </div>
        )}
      </NavLink>
    </aside>
  );
}
