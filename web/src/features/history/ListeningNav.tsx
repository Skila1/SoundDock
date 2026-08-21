import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";

const items = [
  { to: "/history", label: "Recently played", end: true },
  { to: "/history/never-played", label: "Never played" },
  { to: "/history/rediscovery", label: "Rediscovery" },
  { to: "/stats", label: "Stats" },
  { to: "/wrapped", label: "Wrapped" }
];

export function ListeningNav() {
  return (
    <div className="mb-5 flex flex-wrap gap-1">
      {items.map((it) => (
        <NavLink
          key={it.to}
          to={it.to}
          end={it.end}
          className={({ isActive }) =>
            cn("rounded-full px-3 py-1 text-sm", isActive ? "bg-accent text-[#04140a]" : "bg-surface-2 text-muted hover:text-foreground")
          }
        >
          {it.label}
        </NavLink>
      ))}
    </div>
  );
}
