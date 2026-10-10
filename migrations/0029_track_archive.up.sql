-- Archived songs stay in the catalogue, playlists and personal libraries but
-- are hidden from listings until someone searches for and plays them again.
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS tracks_archived_at_idx ON tracks (archived_at) WHERE archived_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS listen_history_track_played_idx ON listen_history (track_id, played_at DESC);
