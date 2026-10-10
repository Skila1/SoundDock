-- What party guests may do on the host's playback. DJs (promoted members) may
-- do everything the host can except change settings or remove people.
ALTER TABLE playback_sessions
    ADD COLUMN IF NOT EXISTS party_permissions JSONB NOT NULL
    DEFAULT '{"add": true, "vote": true, "skip": false, "pause": false}'::jsonb;
