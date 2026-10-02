import {
  Activity,
  ArchiveRestore,
  Disc3,
  HardDrive,
  LayoutDashboard,
  MessageCircle,
  Plug,
  RefreshCw,
  SlidersHorizontal,
  Timer,
  Users,
  UsersRound,
  type LucideIcon
} from "lucide-react";

export type AdminNavLink = { to: string; label: string; icon: LucideIcon };

export type AdminNavGroup = {
  id: string;
  label: string;
  links: readonly AdminNavLink[];
};

export const adminNavGroups: readonly AdminNavGroup[] = [
  {
    id: "overview",
    label: "Overview",
    links: [
      { to: ".", label: "Dashboard", icon: LayoutDashboard },
      { to: "activity", label: "Activity", icon: Activity }
    ]
  },
  {
    id: "server",
    label: "Server",
    links: [
      { to: "backups", label: "Backups", icon: ArchiveRestore },
      { to: "updates", label: "Updates", icon: RefreshCw },
      { to: "integrations", label: "Integrations", icon: Plug }
    ]
  },
  {
    id: "people",
    label: "People",
    links: [
      { to: "users", label: "Users", icon: Users },
      { to: "groups", label: "Groups", icon: UsersRound },
      { to: "discord", label: "Discord", icon: MessageCircle }
    ]
  },
  {
    id: "media",
    label: "Media",
    links: [
      { to: "libraries", label: "Libraries & Storage", icon: HardDrive },
      { to: "catalog", label: "Catalog", icon: Disc3 },
      { to: "media-settings", label: "Media Settings", icon: SlidersHorizontal },
      { to: "retention", label: "Retention", icon: Timer }
    ]
  }
];

/**
 * Old admin URLs that were folded into the pages above. Bookmarks land on the
 * page (and tab) that now holds the same feature.
 */
export const legacyAdminRedirects: Readonly<Record<string, string>> = {
  health: "/admin",
  diagnostics: "/admin",
  database: "/admin",
  maintenance: "/admin",
  workers: "/admin/activity?tab=jobs",
  jobs: "/admin/activity?tab=jobs",
  inspect: "/admin/activity?tab=live",
  logs: "/admin/activity?tab=logs",
  security: "/admin/activity?tab=audit",
  providers: "/admin/integrations?tab=providers",
  webhooks: "/admin/integrations?tab=webhooks",
  quotas: "/admin/users",
  roles: "/admin/groups",
  storage: "/admin/libraries?tab=storage",
  grants: "/admin/libraries?tab=access",
  "duplicate-review": "/admin/catalog?tab=duplicates",
  metadata: "/admin/media-settings",
  lyrics: "/admin/media-settings",
  transcoding: "/admin/media-settings",
  "acquisition-policy": "/admin/media-settings",
  "listen-compare": "/admin/stats-migration?tab=compare",
  "stats-rebuild": "/admin/stats-migration"
};

export function adminPath(to: string) {
  if (to === "." || to === "") return "/admin";
  return `/admin/${to}`;
}

/** Every path the admin nav links to, for tests and sanity checks. */
export function adminNavPaths() {
  return adminNavGroups.flatMap((g) => g.links.map((l) => adminPath(l.to)));
}
