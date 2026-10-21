/** Plain names for group permissions, grouped by what people do with them. */
export type PermissionMeta = { name: string; label: string; help: string };
export type PermissionArea = { id: string; label: string; perms: PermissionMeta[] };

export const permissionAreas: PermissionArea[] = [
  {
    id: "listen",
    label: "Listening",
    perms: [
      { name: "tracks.read", label: "Browse the library", help: "See songs, albums, artists and search." },
      { name: "tracks.stream", label: "Play music", help: "Stream audio in the app and over the API." },
      { name: "playlists.write", label: "Make playlists", help: "Create, edit and share playlists." },
      { name: "history.read", label: "Listening history", help: "See their history and stats." }
    ]
  },
  {
    id: "add",
    label: "Adding music",
    perms: [
      { name: "library.upload", label: "Upload music", help: "Upload files into libraries they can write to." },
      { name: "library.import_url", label: "Import from a link", help: "Download a file from a web address." },
      { name: "providers.connect", label: "Connect other services", help: "Link Spotify, YouTube and similar accounts." },
      { name: "playlists.external_import", label: "Import outside playlists", help: "Bring in and keep syncing playlists from other services." }
    ]
  },
  {
    id: "manage",
    label: "Managing the library",
    perms: [
      { name: "library.create", label: "Add libraries", help: "Create new library folders." },
      { name: "library.migrate", label: "Move libraries", help: "Move files into managed storage." },
      { name: "library.delete", label: "Remove libraries", help: "Remove libraries and their songs from the catalogue." },
      { name: "tracks.delete", label: "Delete songs", help: "Remove songs from the catalogue." },
      { name: "tracks.merge", label: "Merge duplicates", help: "Merge duplicate songs and libraries." },
      { name: "tracks.replace_source", label: "Replace a song's audio", help: "Swap a song's file for a YouTube source." },
      { name: "library.acquire", label: "Downloads and restores", help: "Change the download policy and restore managed files." },
      { name: "lyrics.configure", label: "Lyrics settings", help: "Change where lyrics come from." },
      { name: "stats.rebuild", label: "Rebuild stats", help: "Rebuild listening stats from history." }
    ]
  },
  {
    id: "admin",
    label: "Administration",
    perms: [{ name: "roles.manage", label: "Manage groups", help: "Create groups and choose their permissions." }]
  }
];

const known = new Map(permissionAreas.flatMap((a) => a.perms.map((p) => [p.name, p] as const)));

/**
 * Areas with the server's permission list folded in. Permissions this file
 * does not know about land in "Other" with the server's own description.
 */
export function areasFor(server: { name: string; description?: string }[]): PermissionArea[] {
  const present = new Set(server.map((p) => p.name));
  const areas = permissionAreas
    .map((a) => ({ ...a, perms: a.perms.filter((p) => present.has(p.name)) }))
    .filter((a) => a.perms.length);
  const other = server
    .filter((p) => p.name !== "admin" && !known.has(p.name))
    .map((p) => ({ name: p.name, label: p.description || p.name, help: p.name }));
  if (other.length) areas.push({ id: "other", label: "Other", perms: other });
  return areas;
}

export function permissionLabel(name: string) {
  return known.get(name)?.label || name;
}
