-- Query review follow-up: library-grant filters and Home DISTINCT ON (track_id).

CREATE INDEX IF NOT EXISTS library_grants_user_idx
    ON library_grants (user_id)
    WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS library_grants_role_idx
    ON library_grants (role_id)
    WHERE role_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS listen_history_user_track_played_idx
    ON listen_history (user_id, track_id, played_at DESC);

CREATE INDEX IF NOT EXISTS listen_events_user_track_started_idx
    ON listen_events (user_id, track_id, started_at DESC);
