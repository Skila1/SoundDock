-- Wave 4: shadow listen_events + output segments. Does not drop listen_history.
-- Production readers stay on listen_history until Wave 5.

CREATE TABLE IF NOT EXISTS listen_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playback_instance_id UUID,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('qualify', 'skip')),
    accumulated_listened_ms BIGINT NOT NULL DEFAULT 0,
    listened_ms INT,
    track_duration_ms INT,
    qualified_play BOOLEAN NOT NULL DEFAULT FALSE,
    skipped BOOLEAN NOT NULL DEFAULT FALSE,
    legacy_backfill BOOLEAN NOT NULL DEFAULT FALSE,
    source TEXT NOT NULL DEFAULT 'web' CHECK (source IN ('web', 'discord', 'import')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS listen_events_user_started_idx
    ON listen_events (user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS listen_events_instance_user_idx
    ON listen_events (playback_instance_id, user_id);
CREATE UNIQUE INDEX IF NOT EXISTS listen_events_instance_user_kind_uidx
    ON listen_events (playback_instance_id, user_id, kind)
    WHERE playback_instance_id IS NOT NULL AND kind IN ('qualify', 'skip');

CREATE TABLE IF NOT EXISTS listen_instance_state (
    playback_instance_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    accumulated_ms BIGINT NOT NULL DEFAULT 0,
    last_position_ms INT NOT NULL DEFAULT 0,
    last_playhead_sequence BIGINT NOT NULL DEFAULT 0,
    last_checkpoint_at TIMESTAMPTZ,
    qualified BOOLEAN NOT NULL DEFAULT FALSE,
    skipped BOOLEAN NOT NULL DEFAULT FALSE,
    last_output TEXT CHECK (last_output IS NULL OR last_output IN ('browser', 'discord')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (playback_instance_id, user_id)
);

CREATE TABLE IF NOT EXISTS listen_output_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playback_instance_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    output TEXT NOT NULL CHECK (output IN ('browser', 'discord')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS listen_output_segments_instance_idx
    ON listen_output_segments (playback_instance_id, user_id);

INSERT INTO retention_policies (key, days) VALUES ('listen_events', 0)
ON CONFLICT (key) DO NOTHING;

-- Copy history into shadow events. listened_ms stays NULL (do not fabricate duration).
-- accumulated_listened_ms is 0, not duration_ms - recap minutes that summed duration stay estimated.
INSERT INTO listen_events (
    user_id, track_id, kind,
    accumulated_listened_ms, listened_ms, track_duration_ms,
    qualified_play, skipped, legacy_backfill, source, started_at
)
SELECT
    h.user_id,
    h.track_id,
    'qualify',
    0,
    NULL,
    h.duration_ms,
    TRUE,
    FALSE,
    TRUE,
    CASE WHEN h.source IN ('web', 'discord', 'import') THEN h.source ELSE 'web' END,
    h.played_at
FROM listen_history h;
