-- Media retention: prune SoundDock-owned / ScapeX-acquired files without
-- destroying playlist, history, or favourite relationships.

ALTER TABLE tracks
    ADD COLUMN IF NOT EXISTS acquisition TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS acquisition_ref TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS keep_forever BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS media_unavailable_at TIMESTAMPTZ;

ALTER TABLE libraries
    ADD COLUMN IF NOT EXISTS retention_opt_in BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS tracks_acquisition_ref_idx
    ON tracks (acquisition_ref) WHERE acquisition_ref <> '';
CREATE INDEX IF NOT EXISTS tracks_acquisition_idx
    ON tracks (acquisition) WHERE acquisition <> '';
CREATE INDEX IF NOT EXISTS tracks_media_unavailable_idx
    ON tracks (media_unavailable_at) WHERE media_unavailable_at IS NOT NULL;

-- Inbox keys keep the YouTube video id as the filename.
UPDATE tracks t
SET acquisition = 'youtube',
    acquisition_ref = regexp_replace(split_part(tf.storage_key, '/', 2), '\.[^.]+$', '')
FROM track_files tf
WHERE tf.track_id = t.id
  AND t.acquisition = ''
  AND tf.storage_key ~ '^inbox/[A-Za-z0-9_-]{11}\.[A-Za-z0-9]+$';

CREATE TABLE IF NOT EXISTS retention_exclusions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('track', 'album', 'artist', 'playlist', 'library')),
    target_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, target_id)
);

CREATE TABLE IF NOT EXISTS retention_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID REFERENCES jobs(id) ON DELETE SET NULL,
    dry_run BOOLEAN NOT NULL DEFAULT FALSE,
    mode TEXT NOT NULL DEFAULT '',
    eligible_count INT NOT NULL DEFAULT 0,
    eligible_bytes BIGINT NOT NULL DEFAULT 0,
    deleted_count INT NOT NULL DEFAULT 0,
    reclaimed_bytes BIGINT NOT NULL DEFAULT 0,
    interrupted BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS retention_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID REFERENCES retention_runs(id) ON DELETE CASCADE,
    track_id UUID REFERENCES tracks(id) ON DELETE SET NULL,
    title TEXT NOT NULL DEFAULT '',
    artist TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    last_played_at TIMESTAMPTZ,
    play_count INT NOT NULL DEFAULT 0,
    acquisition TEXT NOT NULL DEFAULT '',
    acquisition_ref TEXT NOT NULL DEFAULT '',
    dry_run BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS retention_events_created_idx ON retention_events (created_at DESC);
CREATE INDEX IF NOT EXISTS retention_runs_started_idx ON retention_runs (started_at DESC);
