export type RadioKind = "library" | "artist" | "album" | "track" | "genre" | "decade" | "quick_mix";

export type RadioResponse = {
  kind: RadioKind | string;
  seed_id: string;
  track_ids: string[];
};

export type RadioSeeds = {
  kinds: string[];
  libraries: { id: string; name: string }[];
  genres: { id: string; name: string }[];
  decades: number[];
};

export type PlaylistFolder = { name: string; count: number };

export type PlaylistListItem = {
  id: string;
  name: string;
  description?: string;
  collaborative?: boolean;
  public?: boolean;
  folder?: string;
  created_at?: string;
  user_id?: string;
  owner_name?: string;
  provider?: string | null;
  sync_mode?: string | null;
  last_sync_status?: string | null;
  is_smart?: boolean;
};

export type ProviderCapabilities = {
  oauth?: boolean;
  manual_token?: boolean;
  list_user_playlists?: boolean;
  public_playlists?: boolean;
  private_playlists?: boolean;
  snapshot?: boolean;
  isrc?: boolean;
};

export function capabilityBlurb(caps?: ProviderCapabilities | null): string {
  if (!caps) return "";
  const parts: string[] = [];
  if (caps.list_user_playlists) parts.push("import your playlists");
  if (caps.private_playlists) parts.push("including private lists");
  if (caps.isrc) parts.push("ISRC matching");
  if (caps.snapshot) parts.push("incremental sync");
  if (caps.oauth) parts.push("OAuth connect");
  if (caps.manual_token) parts.push("paste a user token");
  return parts.join(". ");
}

export type ProviderPlaylist = {
  id: string;
  name: string;
  description?: string;
  owner?: string;
  track_count?: number;
  artwork?: string;
};

export type SmartRules = {
  limit?: number;
  match?: string;
  sort?: string;
  clauses?: { field: string; op: string; value: unknown }[];
};

export type SyncDiffItem = {
  id: string;
  position: number;
  title: string;
  artists?: string;
  album?: string;
  isrc?: string;
  match_status?: string;
  match_confidence?: number | null;
  mapped_track_id?: string | null;
  ignored?: boolean;
};

export type SyncDiff = {
  provider?: string;
  sync_mode?: string;
  status?: string;
  error?: string;
  last_sync_at?: string | null;
  matched: number;
  unmatched: number;
  ignored?: number;
  items: SyncDiffItem[];
};
