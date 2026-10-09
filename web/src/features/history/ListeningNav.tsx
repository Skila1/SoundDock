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
    <div className="mb-6 inline-flex flex-wrap gap-1 rounded-full bg-surface-2/70 p-1 ring-1 ring-inset ring-border">
      {items.map((it) => (
        <NavLink
          key={it.to}
          to={it.to}
          end={it.end}
          className={({ isActive }) =>
            cn("rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors", isActive ? "bg-accent text-[#04140a] shadow-sm" : "text-muted hover:text-foreground")
          }
        >
          {it.label}
        </NavLink>
      ))}
    </div>
  );
}
