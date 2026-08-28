-- Deduplicate null-instance qualify backfill rows from 0015 / EnsureEventsSchema races.
-- Keep one qualify per (user_id, track_id, started_at): prefer legacy_backfill, else min(id).
-- Do not delete skip rows or live qualify rows that have a playback_instance_id.

WITH keepers AS (
    SELECT DISTINCT ON (user_id, track_id, started_at) id
    FROM listen_events
    WHERE kind = 'qualify' AND playback_instance_id IS NULL
    ORDER BY user_id, track_id, started_at, legacy_backfill DESC, id
)
DELETE FROM listen_events le
WHERE le.kind = 'qualify'
  AND le.playback_instance_id IS NULL
  AND le.id NOT IN (SELECT id FROM keepers);

CREATE UNIQUE INDEX IF NOT EXISTS listen_events_null_instance_qualify_uidx
    ON listen_events (user_id, track_id, started_at)
    WHERE kind = 'qualify' AND playback_instance_id IS NULL;
