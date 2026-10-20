-- Albums are optional. Songs that were filed under the catch-all
-- "Unknown Album" get no album, and those placeholder albums are removed.
UPDATE tracks SET album_id = NULL
WHERE album_id IN (SELECT id FROM albums WHERE lower(trim(title)) = 'unknown album')
  AND NOT EXISTS (
    SELECT 1 FROM metadata_locks ml
    WHERE ml.entity_type = 'track' AND ml.entity_id = tracks.id AND ml.field = 'album'
  );

DELETE FROM albums a
WHERE lower(trim(a.title)) = 'unknown album'
  AND NOT EXISTS (SELECT 1 FROM tracks t WHERE t.album_id = a.id);
