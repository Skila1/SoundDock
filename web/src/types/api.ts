export type User = {
  id: string;
  username: string;
  display_name: string;
  email?: string | null;
  is_admin: boolean;
  permissions: string[];
  replaygain_mode: string;
  crossfade_seconds: number;
  target_lufs?: number;
  accent?: string;
  density?: "comfortable" | "compact";
};

export type ArtistRef = { id: string; name: string; role?: string; position?: number };

export type Track = {
  id: string;
  title: string;
  duration_ms?: number;
  track_number?: number;
  disc_number?: number;
  year?: number | null;
  explicit?: boolean | null;
  album_id?: string;
  album?: string;
  library_id?: string;
  artists?: ArtistRef[];
  artist?: string;
  created_at?: string;
  source?: string;
  artwork_url?: string;
};

export type Album = {
  id: string;
  title: string;
  year?: number | null;
  edition_title?: string;
  disc_count?: number;
  is_compilation?: boolean;
  artist?: string;
  tracks?: Track[];
};

export type Artist = { id: string; name: string; albums?: Album[]; tracks?: Track[] };

export type Playlist = {
  id: string;
  name: string;
  description?: string;
  collaborative?: boolean;
  public?: boolean;
  created_at?: string;
  provider?: string | null;
  sync_mode?: string | null;
  last_sync_status?: string | null;
  external?: {
    provider: string;
    sync_mode: string;
    status: string;
    last_sync_at?: string;
    external_id?: string;
    matched: number;
    unmatched: number;
  };
  tracks?: { entry_id: string; position: number; track_id: string; title: string }[];
};

export type Library = {
  id: string;
  name: string;
  kind: string;
  read_only: boolean;
  organisation_mode: string;
  storage_type?: string;
  track_count?: number;
  is_default?: boolean;
};

export type SearchHit = {
  type: "track" | "album" | "artist" | "playlist" | "youtube";
  id: string;
  title: string;
  artist?: string;
  album?: string;
  duration_ms?: number;
  codec?: string;
  year?: number | null;
  score?: number;
  source?: string;
  artwork_url?: string;
};

export type QueueState = {
  id: string;
  status: string;
  volume: number;
  shuffle: boolean;
  repeat: string;
  crossfade_seconds: number;
  replaygain_mode: string;
  current_index: number;
  current_track_id?: string | null;
  position_ms: number;
  items: { id: string; position: number; track_id: string }[];
};

export type Favourite = { type: string; id: string; created_at: string };

export type PartyState = {
  session_id: string;
  enabled: boolean;
  host_user_id?: string;
  guests?: { user_id: string }[];
};

export type DeviceSession = {
  id: string;
  kind: string;
  owner_key: string;
  status: string;
  current_track_id?: string | null;
};
