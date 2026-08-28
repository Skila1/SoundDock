DROP TABLE IF EXISTS retention_events;
DROP TABLE IF EXISTS retention_runs;
DROP TABLE IF EXISTS retention_exclusions;

DROP INDEX IF EXISTS tracks_media_unavailable_idx;
DROP INDEX IF EXISTS tracks_acquisition_idx;
DROP INDEX IF EXISTS tracks_acquisition_ref_idx;

ALTER TABLE libraries DROP COLUMN IF EXISTS retention_opt_in;
ALTER TABLE tracks
    DROP COLUMN IF EXISTS media_unavailable_at,
    DROP COLUMN IF EXISTS keep_forever,
    DROP COLUMN IF EXISTS acquisition_ref,
    DROP COLUMN IF EXISTS acquisition;
