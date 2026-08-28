import { NavLink, Outlet } from "react-router-dom";
import { cn } from "@/lib/utils";

const groups = [
  {
    id: "system",
    label: "System",
    hint: "Server health, workers, and configuration",
    links: [
      [".", "Overview"],
      ["health", "Health"],
      ["workers", "Workers"],
      ["backups", "Backups"],
      ["database", "Database"],
      ["integrations", "API keys"],
      ["providers", "External providers"],
      ["webhooks", "Webhooks"],
      ["security", "Security"],
      ["logs", "Logs"],
      ["updates", "Updates"],
      ["maintenance", "Maintenance"],
      ["diagnostics", "Diagnostics"]
    ]
  },
  {
    id: "access",
    label: "Access",
    hint: "People, groups, and Discord",
    links: [
      ["users", "Users"],
      ["roles", "Groups"],
      ["quotas", "Quotas"],
      ["discord", "Discord"]
    ]
  },
  {
    id: "media",
    label: "Media",
    hint: "Libraries, storage, and playback pipeline",
    links: [
      ["libraries", "Libraries"],
      ["storage", "Storage"],
      ["metadata", "Metadata"],
      ["transcoding", "Transcoding"],
      ["retention", "Retention"]
    ]
  }
] as const;

export function AdminLayout() {
  return (
    <div className="flex flex-col gap-6 lg:flex-row">
      <nav className="lg:w-56 lg:shrink-0">
        <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-subtle">Administration</p>
        <div className="flex gap-1 overflow-x-auto lg:flex-col lg:overflow-visible">
          {groups.map((g) => (
            <div key={g.id} className="min-w-max lg:min-w-0">
              <div className="mb-1 mt-3 px-3 first:mt-0">
                <div className="text-[11px] font-semibold uppercase tracking-wide text-subtle">{g.label}</div>
                <p className="hidden text-[11px] text-muted lg:block">{g.hint}</p>
              </div>
              {g.links.map(([to, label]) => (
                <NavLink
                  key={to}
                  to={to}
                  end={to === "."}
                  className={({ isActive }) =>
                    cn("block whitespace-nowrap rounded-lg px-3 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-foreground", isActive && "bg-surface-2 text-foreground")
                  }
                >
                  {label}
                </NavLink>
              ))}
            </div>
          ))}
        </div>
      </nav>
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}
