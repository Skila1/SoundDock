-- Reverse Wave 1a session columns. Do not drop listen_history or package runtime tables
-- (scrobble_*, track_fingerprints, discord_user_voice) which may predate this migration.

DROP TABLE IF EXISTS playback_command_receipts;

ALTER TABLE discord_voice_runtime
    DROP COLUMN IF EXISTS binding_revision;

ALTER TABLE playback_queue_items
    DROP COLUMN IF EXISTS origin;

ALTER TABLE playback_sessions
    DROP COLUMN IF EXISTS muted,
    DROP COLUMN IF EXISTS volume_restore,
    DROP COLUMN IF EXISTS output_pref,
    DROP COLUMN IF EXISTS autoplay,
    DROP COLUMN IF EXISTS state_revision,
    DROP COLUMN IF EXISTS checkpoint_at,
    DROP COLUMN IF EXISTS duration_ms,
    DROP COLUMN IF EXISTS playback_rate,
    DROP COLUMN IF EXISTS playback_instance_id,
    DROP COLUMN IF EXISTS playhead_sequence,
    DROP COLUMN IF EXISTS renderer_kind,
    DROP COLUMN IF EXISTS renderer_id,
    DROP COLUMN IF EXISTS renderer_generation,
    DROP COLUMN IF EXISTS renderer_heartbeat_at;
