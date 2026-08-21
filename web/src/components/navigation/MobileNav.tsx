import { Home, Library, ListMusic, Search, UserRound } from "lucide-react";
import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";

const tabs = [
  { to: "/", label: "Home", icon: Home, end: true },
  { to: "/search", label: "Search", icon: Search },
  { to: "/albums", label: "Library", icon: Library },
  { to: "/playlists", label: "Playlists", icon: ListMusic },
  { to: "/profile", label: "You", icon: UserRound }
];

export function MobileNav() {
  return (
    <nav className="grid h-14 grid-cols-5 border-t border-border bg-surface-1 md:hidden">
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          end={t.end}
          className={({ isActive }) => cn("flex flex-col items-center justify-center gap-0.5 text-[10px] text-subtle", isActive && "text-accent")}
        >
          <t.icon className="h-5 w-5" />
          {t.label}
        </NavLink>
      ))}
    </nav>
  );
}
