DROP INDEX IF EXISTS listen_history_track_played_idx;
DROP INDEX IF EXISTS tracks_archived_at_idx;
ALTER TABLE tracks DROP COLUMN IF EXISTS archived_at;
