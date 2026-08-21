-- SoundDock initial schema. Expand/contract only; never DROP SCHEMA.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE TABLE schema_migrations_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Identity
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL UNIQUE,
    email TEXT UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    replaygain_mode TEXT NOT NULL DEFAULT 'off',
    crossfade_seconds INT NOT NULL DEFAULT 0,
    target_lufs DOUBLE PRECISION NOT NULL DEFAULT -18,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT,
    ip TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    provider_username TEXT NOT NULL DEFAULT '',
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE identity_link_challenges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT NOT NULL UNIQUE,
    provider TEXT NOT NULL,
    provider_user_id TEXT,
    provider_username TEXT,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);

-- Storage / libraries
CREATE TABLE storage_providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('local', 's3', 'managed')),
    config_enc BYTEA NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE libraries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'music',
    storage_provider_id UUID NOT NULL REFERENCES storage_providers(id),
    root_prefix TEXT NOT NULL DEFAULT '',
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    scan_mode TEXT NOT NULL DEFAULT 'incremental',
    organisation_mode TEXT NOT NULL DEFAULT 'virtual' CHECK (organisation_mode IN ('virtual', 'managed')),
    naming_template TEXT NOT NULL DEFAULT '{album_artist}/{album} ({year})/{disc}{track} - {title}.{ext}',
    allow_physical_reorganise BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE library_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID REFERENCES roles(id) ON DELETE CASCADE,
    actions TEXT[] NOT NULL DEFAULT ARRAY['read','stream'],
    CHECK ((user_id IS NOT NULL) <> (role_id IS NOT NULL))
);

-- Catalogue
CREATE TABLE artists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    sort_name TEXT NOT NULL DEFAULT '',
    mbid TEXT,
    search_vec TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX artists_name_trgm ON artists USING gin (name gin_trgm_ops);
CREATE INDEX artists_search_idx ON artists USING gin (search_vec);

CREATE TABLE albums (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    sort_title TEXT NOT NULL DEFAULT '',
    year INT,
    release_date DATE,
    release_group_mbid TEXT,
    mbid TEXT,
    edition_title TEXT NOT NULL DEFAULT '',
    disc_count INT NOT NULL DEFAULT 1,
    is_compilation BOOLEAN NOT NULL DEFAULT FALSE,
    label TEXT NOT NULL DEFAULT '',
    barcode TEXT NOT NULL DEFAULT '',
    library_id UUID REFERENCES libraries(id) ON DELETE SET NULL,
    search_vec TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX albums_title_trgm ON albums USING gin (title gin_trgm_ops);
CREATE INDEX albums_search_idx ON albums USING gin (search_vec);

CREATE TABLE album_artists (
    album_id UUID NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    artist_id UUID NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'album_artist',
    position INT NOT NULL DEFAULT 0,
    PRIMARY KEY (album_id, artist_id, role)
);

CREATE TABLE tracks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    album_id UUID REFERENCES albums(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    disc_number INT NOT NULL DEFAULT 1,
    track_number INT NOT NULL DEFAULT 0,
    duration_ms INT NOT NULL DEFAULT 0,
    year INT,
    explicit BOOLEAN,
    genre_text TEXT NOT NULL DEFAULT '',
    mbid TEXT,
    isrc TEXT,
    locked BOOLEAN NOT NULL DEFAULT FALSE,
    search_vec TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tracks_title_trgm ON tracks USING gin (title gin_trgm_ops);
CREATE INDEX tracks_search_idx ON tracks USING gin (search_vec);
CREATE INDEX tracks_library_idx ON tracks (library_id);
CREATE INDEX tracks_album_idx ON tracks (album_id, disc_number, track_number);

CREATE TABLE track_artists (
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    artist_id UUID NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'primary',
    position INT NOT NULL DEFAULT 0,
    PRIMARY KEY (track_id, artist_id, role)
);

CREATE TABLE track_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    content_hash TEXT,
    codec TEXT NOT NULL DEFAULT '',
    container TEXT NOT NULL DEFAULT '',
    bitrate INT,
    sample_rate INT,
    channels INT,
    bit_depth INT,
    quality TEXT NOT NULL DEFAULT 'original',
    replaygain_track_gain DOUBLE PRECISION,
    replaygain_track_peak DOUBLE PRECISION,
    replaygain_album_gain DOUBLE PRECISION,
    replaygain_album_peak DOUBLE PRECISION,
    lufs_integrated DOUBLE PRECISION,
    loudness_status TEXT NOT NULL DEFAULT 'pending',
    mtime TIMESTAMPTZ,
    etag TEXT,
    encoder_delay INT,
    encoder_padding INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (library_id, storage_key)
);
CREATE INDEX track_files_hash_idx ON track_files (content_hash);

CREATE TABLE genres (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE track_genres (
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    genre_id UUID NOT NULL REFERENCES genres(id) ON DELETE CASCADE,
    PRIMARY KEY (track_id, genre_id)
);

CREATE TABLE lyrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'embedded',
    timed BOOLEAN NOT NULL DEFAULT FALSE,
    body TEXT NOT NULL
);

CREATE TABLE metadata_locks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    field TEXT NOT NULL,
    locked_by UUID REFERENCES users(id) ON DELETE SET NULL,
    locked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entity_type, entity_id, field)
);

CREATE TABLE artwork_assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_type TEXT NOT NULL,
    owner_id UUID NOT NULL,
    source TEXT NOT NULL DEFAULT 'embedded',
    storage_key TEXT NOT NULL,
    mime TEXT NOT NULL DEFAULT 'image/jpeg',
    width INT,
    height INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX artwork_owner_idx ON artwork_assets (owner_type, owner_id);

CREATE TABLE artwork_derivatives (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artwork_id UUID NOT NULL REFERENCES artwork_assets(id) ON DELETE CASCADE,
    size_name TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    mime TEXT NOT NULL DEFAULT 'image/webp',
    width INT NOT NULL,
    height INT NOT NULL,
    UNIQUE (artwork_id, size_name)
);

-- Ingest
CREATE TABLE upload_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    relative_path TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL,
    offset_bytes BIGINT NOT NULL DEFAULT 0,
    content_hash TEXT,
    staging_key TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'in_progress',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE import_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    dest_library_id UUID REFERENCES libraries(id) ON DELETE SET NULL,
    source_library_id UUID REFERENCES libraries(id) ON DELETE SET NULL,
    options JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'queued',
    progress INT NOT NULL DEFAULT 0,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE import_job_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT
);

-- Playback
CREATE TABLE playback_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    owner_key TEXT NOT NULL,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    volume DOUBLE PRECISION NOT NULL DEFAULT 1,
    repeat_mode TEXT NOT NULL DEFAULT 'off',
    shuffle BOOLEAN NOT NULL DEFAULT FALSE,
    crossfade_seconds INT NOT NULL DEFAULT 0,
    replaygain_mode TEXT NOT NULL DEFAULT 'off',
    current_index INT NOT NULL DEFAULT 0,
    current_track_id UUID REFERENCES tracks(id) ON DELETE SET NULL,
    position_ms INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'stopped',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, owner_key)
);

CREATE TABLE playback_queue_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES playback_sessions(id) ON DELETE CASCADE,
    position INT NOT NULL,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    requested_by_discord_user_id TEXT
);
CREATE INDEX playback_queue_session_idx ON playback_queue_items (session_id, position);

CREATE TABLE playlists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    collaborative BOOLEAN NOT NULL DEFAULT FALSE,
    public BOOLEAN NOT NULL DEFAULT FALSE,
    folder TEXT NOT NULL DEFAULT '',
    search_vec TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE playlist_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position INT NOT NULL,
    added_by UUID REFERENCES users(id) ON DELETE SET NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX playlist_entries_idx ON playlist_entries (playlist_id, position);

CREATE TABLE playlist_collaborators (
    playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (playlist_id, user_id)
);

CREATE TABLE favourites (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, entity_type, entity_id)
);

CREATE TABLE listen_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    played_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms INT NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'web'
);
CREATE INDEX listen_history_user_idx ON listen_history (user_id, played_at DESC);

CREATE TABLE play_counts (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    count INT NOT NULL DEFAULT 0,
    last_played_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, track_id)
);

-- Jobs / ops
CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'queued',
    progress INT NOT NULL DEFAULT 0,
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    last_error TEXT,
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    run_after TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    locked_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX jobs_claim_idx ON jobs (status, run_after) WHERE status IN ('queued', 'retry');

CREATE TABLE job_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    attempt INT NOT NULL,
    error TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE scan_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    job_id UUID REFERENCES jobs(id) ON DELETE SET NULL,
    kind TEXT NOT NULL DEFAULT 'full',
    files_seen INT NOT NULL DEFAULT 0,
    files_added INT NOT NULL DEFAULT 0,
    files_updated INT NOT NULL DEFAULT 0,
    files_removed INT NOT NULL DEFAULT 0,
    files_failed INT NOT NULL DEFAULT 0,
    bytes_seen BIGINT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE scan_file_errors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_run_id UUID NOT NULL REFERENCES scan_runs(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    error TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE duplicate_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    method TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE duplicates (
    group_id UUID NOT NULL REFERENCES duplicate_groups(id) ON DELETE CASCADE,
    track_file_id UUID NOT NULL REFERENCES track_files(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, track_file_id)
);

CREATE TABLE api_clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_client_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES api_clients(id) ON DELETE CASCADE,
    prefix TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE api_client_libraries (
    client_id UUID NOT NULL REFERENCES api_clients(id) ON DELETE CASCADE,
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    PRIMARY KEY (client_id, library_id)
);

CREATE TABLE personal_access_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE api_usage_aggregates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket TIMESTAMPTZ NOT NULL,
    client_id UUID,
    route_class TEXT NOT NULL,
    count INT NOT NULL DEFAULT 0
);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target TEXT,
    ip TEXT,
    meta JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_idx ON audit_events (created_at DESC);

CREATE TABLE server_settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL
);

CREATE TABLE backups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    checksum TEXT,
    status TEXT NOT NULL DEFAULT 'created',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE backup_verifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    backup_id UUID NOT NULL REFERENCES backups(id) ON DELETE CASCADE,
    ok BOOLEAN NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE retention_policies (
    key TEXT PRIMARY KEY,
    days INT NOT NULL
);

CREATE TABLE transcode_cache_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile TEXT NOT NULL,
    track_file_id UUID NOT NULL REFERENCES track_files(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    last_access TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (profile, track_file_id)
);

CREATE TABLE webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    url TEXT NOT NULL,
    secret_enc BYTEA NOT NULL,
    events TEXT[] NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    payload_hash TEXT NOT NULL,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Discord
CREATE TABLE discord_settings (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    bot_token_enc BYTEA,
    application_id TEXT,
    public_key TEXT,
    client_id TEXT,
    client_secret_enc BYTEA,
    last_gateway_status TEXT NOT NULL DEFAULT 'disconnected',
    last_error_redacted TEXT,
    command_registration_status TEXT NOT NULL DEFAULT 'unknown',
    registered_command_hashes TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO discord_settings (id) VALUES (1);

CREATE TABLE discord_guilds (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    icon TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    default_volume DOUBLE PRECISION NOT NULL DEFAULT 1,
    queue_limit INT NOT NULL DEFAULT 500,
    inactivity_leave_empty_minutes INT NOT NULL DEFAULT 5,
    inactivity_leave_no_listeners_minutes INT NOT NULL DEFAULT 10,
    stay_while_queue_nonempty BOOLEAN NOT NULL DEFAULT TRUE,
    allow_personal_playlists BOOLEAN NOT NULL DEFAULT FALSE,
    explicit_policy TEXT NOT NULL DEFAULT 'allow',
    now_playing_channel_id TEXT,
    now_playing_message_id TEXT,
    install_diagnostics JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE discord_guild_libraries (
    guild_id TEXT NOT NULL REFERENCES discord_guilds(id) ON DELETE CASCADE,
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    PRIMARY KEY (guild_id, library_id)
);

CREATE TABLE discord_guild_roles (
    guild_id TEXT NOT NULL REFERENCES discord_guilds(id) ON DELETE CASCADE,
    discord_role_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    PRIMARY KEY (guild_id, discord_role_id)
);

CREATE TABLE discord_guild_channels (
    guild_id TEXT NOT NULL REFERENCES discord_guilds(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    allowed BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (guild_id, channel_id)
);

CREATE TABLE discord_role_libraries (
    guild_id TEXT NOT NULL REFERENCES discord_guilds(id) ON DELETE CASCADE,
    discord_role_id TEXT NOT NULL,
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    PRIMARY KEY (guild_id, discord_role_id, library_id)
);

CREATE TABLE discord_command_policies (
    guild_id TEXT NOT NULL REFERENCES discord_guilds(id) ON DELETE CASCADE,
    command_name TEXT NOT NULL,
    min_level TEXT NOT NULL DEFAULT 'everyone',
    PRIMARY KEY (guild_id, command_name)
);

CREATE TABLE discord_voice_runtime (
    guild_id TEXT PRIMARY KEY REFERENCES discord_guilds(id) ON DELETE CASCADE,
    voice_channel_id TEXT,
    session_id UUID REFERENCES playback_sessions(id) ON DELETE SET NULL,
    connected BOOLEAN NOT NULL DEFAULT FALSE,
    last_disconnect_reason TEXT
);

CREATE TABLE discord_playback_errors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    guild_id TEXT NOT NULL,
    track_id UUID,
    error_class TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Search triggers
CREATE OR REPLACE FUNCTION sounddock_artist_search_vec() RETURNS trigger AS $$
BEGIN
  NEW.search_vec := to_tsvector('simple', unaccent(coalesce(NEW.name,'') || ' ' || coalesce(NEW.sort_name,'')));
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER artists_search_vec BEFORE INSERT OR UPDATE ON artists FOR EACH ROW EXECUTE FUNCTION sounddock_artist_search_vec();

CREATE OR REPLACE FUNCTION sounddock_album_search_vec() RETURNS trigger AS $$
BEGIN
  NEW.search_vec := to_tsvector('simple', unaccent(coalesce(NEW.title,'') || ' ' || coalesce(NEW.edition_title,'')));
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER albums_search_vec BEFORE INSERT OR UPDATE ON albums FOR EACH ROW EXECUTE FUNCTION sounddock_album_search_vec();

CREATE OR REPLACE FUNCTION sounddock_track_search_vec() RETURNS trigger AS $$
BEGIN
  NEW.search_vec := to_tsvector('simple', unaccent(coalesce(NEW.title,'') || ' ' || coalesce(NEW.genre_text,'')));
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER tracks_search_vec BEFORE INSERT OR UPDATE ON tracks FOR EACH ROW EXECUTE FUNCTION sounddock_track_search_vec();

CREATE OR REPLACE FUNCTION sounddock_playlist_search_vec() RETURNS trigger AS $$
BEGIN
  NEW.search_vec := to_tsvector('simple', unaccent(coalesce(NEW.name,'') || ' ' || coalesce(NEW.description,'')));
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER playlists_search_vec BEFORE INSERT OR UPDATE ON playlists FOR EACH ROW EXECUTE FUNCTION sounddock_playlist_search_vec();

-- Seed RBAC
INSERT INTO roles (name, description, is_system) VALUES
  ('Administrator', 'Full server administration', TRUE),
  ('User', 'Browse, stream, playlists, personal settings', TRUE);

INSERT INTO permissions (name, description) VALUES
  ('admin', 'Full administrative access'),
  ('library.create', 'Create libraries'),
  ('library.upload', 'Upload to writable libraries'),
  ('library.import_url', 'Remote URL import'),
  ('library.migrate', 'Migrate libraries into managed storage'),
  ('tracks.stream', 'Stream audio'),
  ('tracks.read', 'Read catalogue'),
  ('playlists.write', 'Create and edit playlists'),
  ('history.read', 'Read listening history');

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p WHERE r.name = 'Administrator';

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p ON p.name IN ('tracks.stream','tracks.read','playlists.write','history.read','library.upload')
WHERE r.name = 'User';

INSERT INTO retention_policies (key, days) VALUES
  ('listen_history', 365),
  ('operational_logs', 90),
  ('failed_jobs', 30),
  ('discord_playback_errors', 30),
  ('api_usage', 90),
  ('audit_events', 730);

INSERT INTO server_settings (key, value) VALUES
  ('setup_complete', 'false'::jsonb),
  ('instance_name', '"SoundDock"'::jsonb),
  ('metadata_external_enabled', 'false'::jsonb),
  ('opensubsonic_enabled', 'false'::jsonb),
  ('transcode_cache_max_bytes', '10737418240'::jsonb),
  ('url_import_max_bytes', '209715200'::jsonb),
  ('metrics_enabled', 'false'::jsonb);
