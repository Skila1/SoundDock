import { NavLink, useLocation } from "react-router-dom";
import { Home, Library, Link2, ListMusic, PanelLeftClose, PanelLeftOpen, Radio, Search, Shield } from "lucide-react";
import { Logo } from "@/components/brand/Logo";
import { Button } from "@/components/ui/button";
import { Tooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useUi } from "@/stores/ui";
import { adminNavGroups, adminPath } from "@/features/admin/adminNav";
import type { User } from "@/types/api";

const primary = [
  { to: "/", label: "Home", icon: Home, end: true },
  { to: "/search", label: "Search", icon: Search },
  { to: "/library", label: "Library", icon: Library },
  { to: "/playlists", label: "Playlists", icon: ListMusic },
  { to: "/radio", label: "Radio", icon: Radio },
  { to: "/settings/connected", label: "Connected Services", icon: Link2 }
];

const listening = [
  { to: "/history", label: "History" },
  { to: "/stats", label: "Stats" },
  { to: "/wrapped", label: "Wrapped" },
  { to: "/profile/party", label: "Party" }
];

export function Sidebar({ user, collapsed, className, collapsible = false }: { user: User; collapsed?: boolean; className?: string; collapsible?: boolean }) {
  const ui = useUi();
  const loc = useLocation();
  const compact = collapsed ?? ui.navCollapsed;
  const adminOpen = !!user.is_admin && loc.pathname.startsWith("/admin");
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
      <nav className="flex-1 space-y-0.5 overflow-auto px-2 pt-2">
        {adminOpen ? (
          <>
            <NavLink
              to="/"
              title={compact ? "Back to app" : undefined}
              className="flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-foreground"
            >
              <Home className="h-4 w-4 shrink-0" />
              {!compact && "Back to app"}
            </NavLink>
            {adminNavGroups.map((g) => (
              <div key={g.id} className={compact ? "pt-2" : "pt-3"}>
                {!compact && (
                  <div className="px-3 pb-1">
                    <div className="text-[11px] font-semibold uppercase tracking-wide text-accent">{g.label}</div>
                    <p className="text-[11px] text-accent/70">{g.hint}</p>
                  </div>
                )}
                {g.links.map(([to, label]) => (
                  <NavLink
                    key={to}
                    to={adminPath(to)}
                    end={to === "."}
                    title={compact ? label : undefined}
                    className={({ isActive }) =>
                      cn(
                        "flex items-center gap-3 rounded-lg px-3 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-foreground",
                        compact && "justify-center px-0 text-xs",
                        isActive && "bg-surface-2 text-foreground"
                      )
                    }
                  >
                    {compact ? label.slice(0, 2) : label}
                  </NavLink>
                ))}
              </div>
            ))}
          </>
        ) : (
          <>
            {primary.map((it) => (
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
            {!compact && (
              <div className="pt-4">
                <div className="px-3 pb-1 text-[11px] text-subtle">Listening</div>
                {listening.map((it) => (
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
            {user.is_admin && (
              <NavLink
                to="/admin"
                title={compact ? "Administration" : undefined}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-foreground",
                    compact ? "justify-center px-0" : "mt-4",
                    isActive && "bg-surface-2 text-foreground"
                  )
                }
              >
                <Shield className="h-4 w-4 shrink-0" />
                {!compact && "Administration"}
              </NavLink>
            )}
          </>
        )}
      </nav>
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
