-- External playlist providers (Spotify, YouTube, SoundCloud, Apple Music).
-- Tokens encrypted at rest. Sync never deletes track files.

CREATE TABLE external_provider_settings (
    provider TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    users_may_connect BOOLEAN NOT NULL DEFAULT TRUE,
    public_import BOOLEAN NOT NULL DEFAULT TRUE,
    client_id TEXT NOT NULL DEFAULT '',
    client_secret_enc BYTEA,
    extra_enc BYTEA,
    default_sync_interval TEXT NOT NULL DEFAULT '6h',
    min_sync_interval TEXT NOT NULL DEFAULT '1h',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO external_provider_settings (provider) VALUES
    ('spotify'), ('youtube'), ('soundcloud'), ('apple_music');

CREATE TABLE oauth_transactions (
    state TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_verifier_enc BYTEA,
    redirect_uri TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE external_provider_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_account_id TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    access_token_enc BYTEA,
    refresh_token_enc BYTEA,
    token_expiry TIMESTAMPTZ,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'connected',
    last_error TEXT NOT NULL DEFAULT '',
    last_successful_sync_at TIMESTAMPTZ,
    connected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, provider)
);

CREATE TABLE external_playlists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_account_id UUID REFERENCES external_provider_accounts(id) ON DELETE SET NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    external_playlist_id TEXT NOT NULL,
    sounddock_playlist_id UUID REFERENCES playlists(id) ON DELETE SET NULL,
    name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    owner_external_id TEXT NOT NULL DEFAULT '',
    artwork_url TEXT NOT NULL DEFAULT '',
    track_count INT NOT NULL DEFAULT 0,
    external_snapshot TEXT NOT NULL DEFAULT '',
    sync_mode TEXT NOT NULL DEFAULT 'once',
    sync_interval TEXT NOT NULL DEFAULT '6h',
    removal_policy TEXT NOT NULL DEFAULT 'mirror',
    last_sync_attempt_at TIMESTAMPTZ,
    last_sync_at TIMESTAMPTZ,
    next_sync_at TIMESTAMPTZ,
    last_sync_status TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    UNIQUE (user_id, provider, external_playlist_id)
);

CREATE TABLE external_playlist_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_playlist_id UUID NOT NULL REFERENCES external_playlists(id) ON DELETE CASCADE,
    position INT NOT NULL,
    provider_track_id TEXT NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    artists TEXT NOT NULL DEFAULT '',
    album TEXT NOT NULL DEFAULT '',
    duration_ms INT NOT NULL DEFAULT 0,
    isrc TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    mapped_track_id UUID REFERENCES tracks(id) ON DELETE SET NULL,
    match_status TEXT NOT NULL DEFAULT 'unmatched',
    match_confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    ignored BOOLEAN NOT NULL DEFAULT FALSE,
    item_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX external_playlist_items_idx ON external_playlist_items (external_playlist_id, position);

CREATE TABLE external_track_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    provider_track_id TEXT NOT NULL,
    isrc TEXT NOT NULL DEFAULT '',
    sounddock_track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    mapping_source TEXT NOT NULL DEFAULT 'auto',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    confirmed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_track_id)
);

CREATE TABLE external_sync_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_playlist_id UUID REFERENCES external_playlists(id) ON DELETE CASCADE,
    job_id UUID,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    added_count INT NOT NULL DEFAULT 0,
    removed_count INT NOT NULL DEFAULT 0,
    reordered BOOLEAN NOT NULL DEFAULT FALSE,
    matched_count INT NOT NULL DEFAULT 0,
    unmatched_count INT NOT NULL DEFAULT 0,
    ambiguous_count INT NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE external_sync_errors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID REFERENCES external_sync_runs(id) ON DELETE CASCADE,
    item_id UUID,
    error_class TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO permissions (name, description) VALUES
    ('providers.connect', 'Connect external playlist providers'),
    ('playlists.external_import', 'Import and sync external playlists')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name IN ('Administrator', 'User')
  AND p.name IN ('providers.connect', 'playlists.external_import')
ON CONFLICT DO NOTHING;
