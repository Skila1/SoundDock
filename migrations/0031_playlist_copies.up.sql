-- Playlists made with "Create playlist based on" are the user's own SoundDock
-- playlists. Mark the ones created before sync_mode 'copy' existed, found from
-- the import jobs that made them, so they are listed with SoundDock playlists.
UPDATE external_playlists ep
SET sync_mode = 'copy'
WHERE ep.sync_mode = 'once'
  AND EXISTS (
    SELECT 1 FROM jobs j
    WHERE j.type = 'external.playlist.import'
      AND j.payload->>'copy' = 'true'
      AND j.payload->>'user_id' = ep.user_id::text
      AND j.payload->>'provider' = ep.provider
      AND j.payload->>'external_id' = ep.external_playlist_id
  );
