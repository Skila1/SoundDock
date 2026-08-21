import { NavLink, Outlet } from "react-router-dom";
import { cn } from "@/lib/utils";

const links = [
  [".", "Overview"],
  ["health", "Health"],
  ["users", "Users"],
  ["roles", "Roles"],
  ["storage", "Storage"],
  ["libraries", "Libraries"],
  ["grants", "Grants"],
  ["quotas", "Quotas"],
  ["jobs", "Jobs"],
  ["backups", "Backups"],
  ["backup-preview", "Backup preview"],
  ["database", "Database"],
  ["discord", "Discord"],
  ["integrations", "Integrations"],
  ["providers", "External providers"],
  ["webhooks", "Webhooks"],
  ["metadata", "Metadata"],
  ["transcoding", "Transcoding"],
  ["retention", "Retention"],
  ["security", "Security"],
  ["logs", "Logs"],
  ["cloudflare", "Cloudflare"],
  ["updates", "Updates"],
  ["maintenance", "Maintenance"],
  ["diagnostics", "Diagnostics"],
  ["demo", "Demo"]
];

export function AdminLayout() {
  return (
    <div className="flex flex-col gap-6 lg:flex-row">
      <nav className="flex gap-1 overflow-x-auto lg:w-48 lg:flex-col lg:overflow-visible">
        {links.map(([to, label]) => (
          <NavLink
            key={to}
            to={to}
            end={to === "."}
            className={({ isActive }) =>
              cn("whitespace-nowrap rounded-lg px-3 py-2 text-sm text-muted hover:bg-surface-2", isActive && "bg-surface-2 text-foreground")
            }
          >
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}
