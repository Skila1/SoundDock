import {
  Activity,
  Archive,
  ArchiveRestore,
  Cog,
  Disc3,
  HardDrive,
  KeyRound,
  LayoutDashboard,
  Library,
  MessageCircle,
  Plug,
  RefreshCw,
  ScanSearch,
  SlidersHorizontal,
  Timer,
  Users,
  UsersRound,
  type LucideIcon
} from "lucide-react";

export type AdminNavLink = { to: string; label: string; icon: LucideIcon };

/** One button on the admin bar; its links show as sub-pages under it. */
export type AdminNavGroup = {
  id: string;
  label: string;
  icon: LucideIcon;
  links: readonly AdminNavLink[];
};

export const adminNavGroups: readonly AdminNavGroup[] = [
  {
    id: "overview",
    label: "Overview",
    icon: LayoutDashboard,
    links: [
      { to: ".", label: "Dashboard", icon: LayoutDashboard },
      { to: "activity", label: "Activity", icon: Activity }
    ]
  },
  {
    id: "library",
    label: "Library",
    icon: Library,
    links: [
      { to: "libraries", label: "Libraries", icon: Library },
      { to: "scan", label: "Scan", icon: ScanSearch },
      { to: "catalog", label: "Catalog", icon: Disc3 },
      { to: "media-settings", label: "Media Settings", icon: SlidersHorizontal }
    ]
  },
  {
    id: "storage",
    label: "Storage",
    icon: HardDrive,
    links: [
      { to: "storage", label: "Storage", icon: HardDrive },
      { to: "retention", label: "Retention", icon: Timer },
      { to: "archive", label: "Archive", icon: Archive },
      { to: "backups", label: "Backups", icon: ArchiveRestore }
    ]
  },
  {
    id: "iam",
    label: "IAM",
    icon: KeyRound,
    links: [
      { to: "users", label: "Users", icon: Users },
      { to: "groups", label: "Groups", icon: UsersRound },
      { to: "discord", label: "Discord", icon: MessageCircle }
    ]
  },
  {
    id: "system",
    label: "System",
    icon: Cog,
    links: [
      { to: "updates", label: "Updates", icon: RefreshCw },
      { to: "integrations", label: "Integrations", icon: Plug }
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
  grants: "/admin/libraries?tab=access",
  "duplicate-review": "/admin/scan?tab=duplicates",
  connected: "/settings/connected",
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

/** The bar button whose pages include this path (falls back to Overview). */
export function adminGroupFor(pathname: string) {
  const seg = pathname.replace(/^\/admin\/?/, "").split("/")[0] || ".";
  const page = seg === "users" || seg === "discord-users" ? "users" : seg === "stats-migration" ? "." : seg;
  return adminNavGroups.find((g) => g.links.some((l) => l.to === page)) || adminNavGroups[0];
}

/** Every path the admin nav links to, for tests and sanity checks. */
export function adminNavPaths() {
  return adminNavGroups.flatMap((g) => g.links.map((l) => adminPath(l.to)));
}
