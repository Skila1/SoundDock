-- Library features for Wave 1 agents. Additive only.

ALTER TABLE playback_sessions
    ADD COLUMN IF NOT EXISTS device_id TEXT,
    ADD COLUMN IF NOT EXISTS shuffle_mode TEXT NOT NULL DEFAULT 'random',
    ADD COLUMN IF NOT EXISTS stop_after_current BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS last_device TEXT,
    ADD COLUMN IF NOT EXISTS party_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS party_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS party_host_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE play_counts
    ADD COLUMN IF NOT EXISTS skip_count INT NOT NULL DEFAULT 0;

ALTER TABLE tracks
    ADD COLUMN IF NOT EXISTS metadata_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS metadata_confidence DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS manual_gain_db DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS waveform_peaks JSONB;

ALTER TABLE track_files
    ADD COLUMN IF NOT EXISTS original_size_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS party_members (
    session_id UUID NOT NULL REFERENCES playback_sessions(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'guest',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);

CREATE TABLE IF NOT EXISTS party_votes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES playback_sessions(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS party_votes_session_idx ON party_votes (session_id, created_at DESC);

CREATE TABLE IF NOT EXISTS playlist_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    entries JSONB NOT NULL DEFAULT '[]'::jsonb
);
CREATE INDEX IF NOT EXISTS playlist_snapshots_playlist_idx ON playlist_snapshots (playlist_id, created_at DESC);

CREATE TABLE IF NOT EXISTS smart_playlist_rules (
    playlist_id UUID PRIMARY KEY REFERENCES playlists(id) ON DELETE CASCADE,
    rules JSONB NOT NULL DEFAULT '{}'::jsonb,
    refresh_interval_seconds INT NOT NULL DEFAULT 86400,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO server_settings (key, value) VALUES
    ('announcement', '""'::jsonb),
    ('maintenance', 'false'::jsonb),
    ('stream_remote_max_bitrate', '192'::jsonb),
    ('stream_lan_max_bitrate', '0'::jsonb),
    ('stream_remote_concurrency', '8'::jsonb),
    ('hires_bit_depth_min', '24'::jsonb),
    ('hires_sample_rate_min', '48000'::jsonb),
    ('compression_preset', '"standard"'::jsonb)
ON CONFLICT (key) DO NOTHING;
