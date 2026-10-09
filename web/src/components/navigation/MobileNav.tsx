import { Home, Library, ListMusic, Search, UserRound } from "lucide-react";
import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";

const tabs = [
  { to: "/", label: "Home", icon: Home, end: true },
  { to: "/me/library", label: "My Library", icon: Library },
  { to: "/playlists", label: "My Playlists", icon: ListMusic },
  { to: "/search", label: "Search", icon: Search },
  { to: "/profile", label: "You", icon: UserRound }
];

export function MobileNav() {
  return (
    <nav className="grid h-[calc(4rem+env(safe-area-inset-bottom))] shrink-0 grid-cols-5 border-t border-border bg-surface-1/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden">
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          end={t.end}
          className={({ isActive }) => cn("group flex flex-col items-center justify-center gap-1 text-[10px] font-medium text-subtle transition-colors", isActive && "text-foreground")}
        >
          {({ isActive }) => (
            <>
              <span className={cn("flex h-7 w-12 items-center justify-center rounded-full transition-colors", isActive && "bg-accent/15 text-accent")}>
                <t.icon className="h-5 w-5" />
              </span>
              {t.label}
            </>
          )}
        </NavLink>
      ))}
    </nav>
  );
}
