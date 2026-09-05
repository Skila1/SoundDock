export type AdminNavLink = [to: string, label: string];

export type AdminNavGroup = {
  id: string;
  label: string;
  hint: string;
  links: readonly AdminNavLink[];
};

export const adminNavGroups: readonly AdminNavGroup[] = [
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
      ["inspect", "Inspect"],
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
      ["grants", "Grants"],
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
      ["lyrics", "Lyrics"],
      ["transcoding", "Transcoding"],
      ["retention", "Retention"],
      ["listen-compare", "Listen compare"],
      ["stats-rebuild", "Stats rebuild"],
      ["acquisition-policy", "Acquisition"],
      ["duplicate-review", "Duplicates"]
    ]
  }
];

export function adminPath(to: string) {
  if (to === "." || to === "") return "/admin";
  return `/admin/${to}`;
}
