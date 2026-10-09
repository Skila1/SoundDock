import { NavLink } from "react-router-dom";
import { Link2, MonitorSmartphone, UserRound } from "lucide-react";
import { cn } from "@/lib/utils";

const tabs = [
  { to: "/profile", label: "Profile", icon: UserRound, end: true },
  { to: "/settings/connected", label: "Connected Services", icon: Link2 },
  { to: "/profile/devices", label: "Devices", icon: MonitorSmartphone }
];

/** Shared tab bar for the personal settings pages. */
export function SettingsTabs() {
  return (
    <nav className="-mt-2 mb-7 flex gap-1 overflow-x-auto border-b border-border" aria-label="Settings">
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          end={t.end}
          className={({ isActive }) =>
            cn(
              "relative flex shrink-0 items-center gap-2 px-3 pb-3 pt-1 text-sm font-medium transition-colors",
              isActive
                ? "text-foreground after:absolute after:inset-x-2 after:-bottom-px after:h-0.5 after:rounded-full after:bg-accent"
                : "text-muted hover:text-foreground"
            )
          }
        >
          <t.icon className="h-4 w-4" />
          {t.label}
        </NavLink>
      ))}
    </nav>
  );
}
