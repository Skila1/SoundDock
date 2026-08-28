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

export type MediaState = "ready" | "restoring" | "missing_external";

export type Playability = {
  state: MediaState;
  intent_id?: string;
};

export type AcquisitionPolicy = {
  media_policy_id: string;
  format_profile: string;
};

export type QueueListener = {
  user_id?: string | null;
  display_name: string;
  avatar_url?: string | null;
  source: "web" | "discord" | "both";
};

export type RequestedBy = {
  user_id?: string;
  discord_user_id?: string;
  display_name?: string;
};

export type QueueUndoItem = {
  id: string;
  track_id: string;
  position: number;
  origin?: string;
  requested_by_user_id?: string | null;
  requested_by_discord_user_id?: string | null;
  requested_by?: RequestedBy;
};

export type QueueUndo = {
  undo_generation: number;
  items: QueueUndoItem[];
};

export type QueueState = {
  id: string;
  kind?: string;
  owner_key?: string;
  status: string;
  volume: number;
  shuffle: boolean;
  repeat: string;
  crossfade_seconds: number;
  replaygain_mode: string;
  current_index: number;
  current_track_id?: string | null;
  current_media_state?: MediaState;
  current_intent_id?: string | null;
  position_ms: number;
  items: { id: string; position: number; track_id: string; origin?: string; media_state?: MediaState; intent_id?: string; requested_by?: RequestedBy }[];
  undo?: QueueUndo;
  undo_generation?: number;
  shuffle_mode?: string;
  stop_after_current?: boolean;
  device_id?: string | null;
  state_revision?: number;
  playhead_sequence?: number;
  playback_instance_id?: string | null;
  muted?: boolean;
  output_pref?: string;
  autoplay?: boolean;
  renderer_kind?: string;
  renderer_id?: string | null;
  renderer_generation?: number;
  checkpoint_at?: string | null;
  duration_ms?: number;
  playback_rate?: number;
  renderer_heartbeat_at?: string | null;
  binding_revision?: number | null;
  server_time?: string | null;
  listeners?: QueueListener[];
};

export type SessionStateEvent = Pick<
  QueueState,
  | "id"
  | "kind"
  | "owner_key"
  | "state_revision"
  | "status"
  | "volume"
  | "muted"
  | "output_pref"
  | "playback_instance_id"
  | "repeat"
  | "shuffle"
  | "autoplay"
  | "current_index"
  | "current_track_id"
  | "items"
  | "renderer_kind"
  | "renderer_id"
  | "renderer_generation"
  | "binding_revision"
  | "stop_after_current"
  | "shuffle_mode"
  | "crossfade_seconds"
  | "replaygain_mode"
  | "device_id"
> & { generation?: number };

export type SessionPlayheadEvent = {
  playback_instance_id?: string | null;
  checkpoint_position_ms: number;
  checkpoint_at?: string | null;
  status: string;
  playhead_sequence: number;
  playback_rate: number;
  duration_ms: number;
};

export type SessionPresenceEvent = { listeners: QueueListener[] };

export type AcquisitionStatusEvent = { intents: unknown[] };

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

/** Side-by-side counts. delta is history minus events, never a sum. */
export type ListenComparePair = {
  history: number;
  events: number;
  delta: number;
};

/** Admin dual-read report. Never a merged listen total. */
export type ListenCompareReport = {
  ready: boolean;
  message?: string;
  note: string;
  period: {
    from?: string | null;
    to?: string | null;
    preset: "last_30_days" | "all" | "custom" | string;
    note: string;
  };
  history: {
    row_count: number;
    row_count_excluding_import: number;
    import_row_count: number;
    distinct_users: number;
    distinct_users_excluding_import: number;
    distinct_tracks: number;
    distinct_tracks_excluding_import: number;
    duration_ms_sum: number;
    duration_ms_sum_excluding_import: number;
    estimated_minutes: number;
    estimated_minutes_excluding_import: number;
    estimated_minutes_import: number;
    estimated_minutes_source: string;
  };
  events: {
    row_count: number;
    qualified_play: number;
    qualified_play_live: number;
    qualified_play_backfill: number;
    skipped: number;
    kind_skip: number;
    kind_skip_unqualified: number;
    legacy_backfill: number;
    live: number;
    distinct_users: number;
    distinct_tracks: number;
    listened_ms_sum: number;
    listened_ms_incomplete: boolean;
    null_listened_ms_count: number;
    listened_minutes_incomplete: number;
    output_segment_count: number;
    listened_ms_note: string;
  };
  diffs: {
    match_key: string;
    match_key_note: string;
    delta_meaning: string;
    history_plays_vs_qualifies_live: ListenComparePair;
    history_plays_vs_qualifies_including_backfill: ListenComparePair;
    history_rows_with_no_matching_event: number;
    live_events_with_no_matching_history: number;
    play_counts_skip_count: number;
    skip_events: number;
    skip_events_unqualified: number;
    skip_delta: number;
    skip_note: string;
  } | null;
};

/** Latest stats.rebuild job. Cancel is not offered on the rebuild page during cutover. */
export type StatsRebuildJob = {
  id: string;
  status: string;
  progress: number;
  last_error?: string | null;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
};

/** Current production listen reader and rebuild job. Never a merged listen total. */
export type StatsRebuildStatus = {
  listen_reader: "history" | "events" | string;
  busy: boolean;
  job?: StatsRebuildJob | null;
};

export type StatsRebuildEnqueue = {
  job_id: string;
};

export type ReplaceSourceRequest = {
  url?: string;
  source_ref?: string;
  provider?: string;
};

export type ReplaceSourceEnqueue = {
  job_id: string;
  track_id?: string;
  coalesce_key?: string;
};

export type TrackInUseError = {
  code: "track_in_use" | string;
  message: string;
};

export type DuplicateReviewTrack = {
  id: string;
  title?: string;
  artist?: string;
  duration_ms?: number;
  duration?: number;
};

export type DuplicateReviewGroup = {
  id: string;
  group_id?: string;
  status?: "open" | "merged" | "ignored" | string;
  reason?: string;
  tracks?: DuplicateReviewTrack[];
};

export type DuplicateReviewMergeRequest = {
  winner_id: string;
  loser_ids: string[];
};

export type LyricsLine = {
  t_ms: number;
  text: string;
};

export type TrackLyrics = {
  body: string;
  timed: boolean;
  source: string;
  lines?: LyricsLine[];
};

export type LyricsProviderConfig = {
  enabled: boolean;
  provider_url: string;
};

/** Scoped ACL row on a library. Capabilities (upload, merge, …) remain HasPerm. */
export type LibraryGrant = {
  id: string;
  library_id?: string;
  kind: "role" | "user" | string;
  actions?: string[];
  user_id?: string | null;
  role_id?: string | null;
  username?: string | null;
  role?: string | null;
};

/** server_settings.library_grants_strict — missing key is false (empty actions still grant read+stream). */
export type LibraryGrantsStrict = {
  library_grants_strict: boolean;
};

