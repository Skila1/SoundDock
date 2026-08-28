-- Wave 1a: playback session engine (revision, playhead, renderer lease, receipts).
-- Additive only. Does not drop listen_history.

ALTER TABLE playback_sessions
    ADD COLUMN IF NOT EXISTS muted BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS volume_restore DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS output_pref TEXT NOT NULL DEFAULT 'browser',
    ADD COLUMN IF NOT EXISTS autoplay BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS state_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS checkpoint_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS duration_ms INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS playback_rate DOUBLE PRECISION NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS playback_instance_id UUID,
    ADD COLUMN IF NOT EXISTS playhead_sequence BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS renderer_kind TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS renderer_id TEXT,
    ADD COLUMN IF NOT EXISTS renderer_generation BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS renderer_heartbeat_at TIMESTAMPTZ;

-- status may be 'interrupted'; no CHECK is added.

ALTER TABLE playback_queue_items
    ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'user';

-- Position is ordered but not unique: autoplay/radio inserts may share a slot while reshaping.
ALTER TABLE discord_voice_runtime
    ADD COLUMN IF NOT EXISTS binding_revision BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS playback_command_receipts (
    session_id UUID NOT NULL REFERENCES playback_sessions(id) ON DELETE CASCADE,
    command_id TEXT NOT NULL,
    action TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    result_status INT NOT NULL,
    result_code TEXT,
    resulting_revision BIGINT,
    resulting_item_id UUID,
    result_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, command_id)
);

-- Runtime tables previously created ad-hoc by packages. Match those CREATE statements.
CREATE TABLE IF NOT EXISTS scrobble_accounts (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    lastfm_username TEXT NOT NULL DEFAULT '',
    lastfm_session_enc BYTEA,
    listenbrainz_username TEXT NOT NULL DEFAULT '',
    listenbrainz_token_enc BYTEA,
    presence_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS scrobble_listen_state (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    track_id UUID NOT NULL,
    counted BOOLEAN NOT NULL DEFAULT FALSE,
    max_position_ms INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, source)
);

CREATE TABLE IF NOT EXISTS track_fingerprints (
    track_file_id UUID PRIMARY KEY REFERENCES track_files(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    duration_seconds DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS discord_user_voice (
    discord_user_id TEXT NOT NULL,
    guild_id TEXT NOT NULL,
    channel_id TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (discord_user_id, guild_id)
);

CREATE TABLE IF NOT EXISTS discord_voice_runtime (
    guild_id TEXT PRIMARY KEY REFERENCES discord_guilds(id) ON DELETE CASCADE,
    voice_channel_id TEXT,
    session_id UUID REFERENCES playback_sessions(id) ON DELETE SET NULL,
    connected BOOLEAN NOT NULL DEFAULT FALSE,
    last_disconnect_reason TEXT,
    binding_revision BIGINT NOT NULL DEFAULT 0
);
