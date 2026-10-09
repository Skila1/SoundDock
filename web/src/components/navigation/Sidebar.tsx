import { NavLink, useLocation } from "react-router-dom";
import { BarChart3, Disc3, History, Home, Library, Link2, ListMusic, PanelLeftClose, PanelLeftOpen, Radio, Shield, Sparkles, Users, type LucideIcon } from "lucide-react";
import { Logo } from "@/components/brand/Logo";
import { Button } from "@/components/ui/button";
import { Tooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useUi } from "@/stores/ui";
import { adminNavGroups, adminPath } from "@/features/admin/adminNav";
import type { User } from "@/types/api";

type NavItem = { to: string; label: string; icon: LucideIcon; end?: boolean };

const groups: { id: string; label?: string; items: NavItem[] }[] = [
  {
    id: "listen",
    items: [
      { to: "/", label: "Home", icon: Home, end: true },
      { to: "/radio", label: "Radio", icon: Radio }
    ]
  },
  {
    id: "library",
    label: "Library",
    items: [
      { to: "/me/library", label: "My Library", icon: Library },
      { to: "/playlists", label: "My Playlists", icon: ListMusic },
      { to: "/library", label: "Catalogue", icon: Disc3 }
    ]
  },
  {
    id: "you",
    label: "You",
    items: [
      { to: "/history", label: "History", icon: History },
      { to: "/stats", label: "Stats", icon: BarChart3 },
      { to: "/wrapped", label: "Wrapped", icon: Sparkles },
      { to: "/profile/party", label: "Party", icon: Users }
    ]
  }
];

const itemClass = (isActive: boolean, compact: boolean) =>
  cn(
    "relative flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted transition-colors hover:bg-surface-2/70 hover:text-foreground",
    compact && "justify-center px-0",
    isActive &&
      "bg-surface-2 text-foreground before:absolute before:left-0 before:top-1/2 before:h-5 before:w-[3px] before:-translate-y-1/2 before:rounded-full before:bg-accent [&>svg]:text-accent"
  );

function NavRow({ item, compact }: { item: NavItem; compact: boolean }) {
  return (
    <NavLink
      to={item.to}
      end={item.end}
      title={compact ? item.label : undefined}
      aria-label={compact ? item.label : undefined}
      className={({ isActive }) => itemClass(isActive, compact)}
    >
      <item.icon className="h-[18px] w-[18px] shrink-0" />
      {!compact && <span className="truncate">{item.label}</span>}
    </NavLink>
  );
}

export function Sidebar({ user, collapsed, className, collapsible = false }: { user: User; collapsed?: boolean; className?: string; collapsible?: boolean }) {
  const ui = useUi();
  const loc = useLocation();
  const compact = collapsed ?? ui.navCollapsed;
  const adminOpen = !!user.is_admin && loc.pathname.startsWith("/admin");
  return (
    <aside className={cn("h-full min-h-0 flex-col overflow-hidden border-r border-border bg-surface-1/80", className || "hidden md:flex", compact ? "w-[72px]" : "w-[232px]")}>
      <div className="flex items-center bg-black">
        <NavLink to="/" end aria-label="SoundDock home" className={cn("flex min-w-0 flex-1 items-center px-4 py-3", compact && "justify-center px-2")}>
          <Logo mark={compact} className={compact ? "h-10 w-10" : "h-14 w-auto max-w-[176px]"} />
        </NavLink>
        {collapsible && !compact && (
          <Tooltip label="Collapse menu">
            <Button size="icon" variant="ghost" className="mr-2 h-8 w-8 text-white hover:bg-white/10" onClick={() => ui.set({ navCollapsed: true })} aria-label="Collapse menu">
              <PanelLeftClose className="h-4 w-4" />
            </Button>
          </Tooltip>
        )}
      </div>
      <nav className="flex-1 overflow-auto px-2.5 pb-3 pt-3">
        {adminOpen ? (
          <>
            <NavLink
              to="/"
              title={compact ? "Back to app" : undefined}
              className="relative flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted transition-colors hover:bg-surface-2/70 hover:text-foreground"
            >
              <Home className="h-4 w-4 shrink-0" />
              {!compact && "Back to app"}
            </NavLink>
            {adminNavGroups.map((g) => (
              <div key={g.id} className={compact ? "pt-2" : "pt-3"}>
                {!compact && (
                  <div className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wide text-accent">{g.label}</div>
                )}
                {g.links.map(({ to, label, icon: Icon }) => (
                  <NavLink
                    key={to}
                    to={adminPath(to)}
                    end={to === "."}
                    title={compact ? label : undefined}
                    aria-label={compact ? label : undefined}
                    className={({ isActive }) =>
                      cn(
                        "relative flex items-center gap-3 rounded-lg px-3 py-1.5 text-sm text-muted transition-colors hover:bg-surface-2/70 hover:text-foreground",
                        compact && "justify-center px-0 py-2",
                        isActive && "bg-surface-2 text-foreground before:absolute before:left-0 before:top-1/2 before:h-5 before:w-[3px] before:-translate-y-1/2 before:rounded-full before:bg-accent [&>svg]:text-accent"
                      )
                    }
                  >
                    <Icon className="h-4 w-4 shrink-0" />
                    {!compact && label}
                  </NavLink>
                ))}
              </div>
            ))}
          </>
        ) : (
          <>
            {groups.map((g, gi) => (
              <div key={g.id} className={cn(gi > 0 && (compact ? "mt-2 border-t border-border pt-2" : "pt-5"))}>
                {!compact && g.label && (
                  <div className="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-subtle">{g.label}</div>
                )}
                <div className="space-y-0.5">
                  {g.items.map((it) => (
                    <NavRow key={it.to} item={it} compact={compact} />
                  ))}
                </div>
              </div>
            ))}
            <div className={cn(compact ? "mt-2 border-t border-border pt-2" : "pt-5", "space-y-0.5")}>
              {!compact && <div className="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-subtle">Settings</div>}
              <NavRow item={{ to: "/settings/connected", label: "Connected Services", icon: Link2 }} compact={compact} />
              {user.is_admin && <NavRow item={{ to: "/admin", label: "Administration", icon: Shield }} compact={compact} />}
            </div>
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
      <NavLink to="/profile" className={cn("m-3 flex items-center gap-3 rounded-xl bg-surface-2/70 px-3 py-2 ring-1 ring-inset ring-border transition hover:bg-surface-2", compact && "justify-center px-2")}>
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
