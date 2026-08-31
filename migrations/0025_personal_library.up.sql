-- Personal mini-library: per-identity references to canonical tracks.
-- Visibility lives on users for web accounts and on owners for Discord-only identities.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS personal_library_visibility TEXT NOT NULL DEFAULT 'private';

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_personal_library_visibility_chk;

ALTER TABLE users
    ADD CONSTRAINT users_personal_library_visibility_chk
    CHECK (personal_library_visibility IN ('private', 'public'));

CREATE TABLE IF NOT EXISTS personal_library_owners (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    discord_user_id TEXT UNIQUE,
    visibility TEXT NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'public')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT personal_library_owners_identity_chk
        CHECK (user_id IS NOT NULL OR discord_user_id IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS personal_library_entries (
    owner_id UUID NOT NULL REFERENCES personal_library_owners(id) ON DELETE CASCADE,
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    first_requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_count INT NOT NULL DEFAULT 1,
    PRIMARY KEY (owner_id, track_id)
);

CREATE INDEX IF NOT EXISTS personal_library_entries_last_idx
    ON personal_library_entries (owner_id, last_requested_at DESC);

INSERT INTO personal_library_owners (user_id, visibility)
SELECT id, personal_library_visibility FROM users
ON CONFLICT (user_id) DO NOTHING;

UPDATE personal_library_owners o
SET discord_user_id = i.provider_user_id, updated_at = now()
FROM user_identities i
WHERE i.provider = 'discord'
  AND i.user_id = o.user_id
  AND o.discord_user_id IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM personal_library_owners x
      WHERE x.discord_user_id = i.provider_user_id
  );

INSERT INTO personal_library_owners (discord_user_id)
SELECT DISTINCT q.requested_by_discord_user_id
FROM playback_queue_items q
WHERE coalesce(q.requested_by_discord_user_id, '') <> ''
  AND q.requested_by_user_id IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM personal_library_owners o
      WHERE o.discord_user_id = q.requested_by_discord_user_id
  );

INSERT INTO personal_library_entries (owner_id, track_id, first_requested_at, last_requested_at, request_count)
SELECT o.id, q.track_id, now(), now(), COUNT(*)::int
FROM playback_queue_items q
JOIN personal_library_owners o ON (
    (q.requested_by_user_id IS NOT NULL AND o.user_id = q.requested_by_user_id)
    OR (
        q.requested_by_user_id IS NULL
        AND coalesce(q.requested_by_discord_user_id, '') <> ''
        AND o.discord_user_id = q.requested_by_discord_user_id
    )
)
WHERE coalesce(q.origin, 'user') = 'user'
  AND q.track_id IS NOT NULL
GROUP BY o.id, q.track_id
ON CONFLICT (owner_id, track_id) DO UPDATE SET
    request_count = personal_library_entries.request_count + EXCLUDED.request_count,
    last_requested_at = GREATEST(personal_library_entries.last_requested_at, EXCLUDED.last_requested_at);

INSERT INTO personal_library_entries (owner_id, track_id, first_requested_at, last_requested_at, request_count)
SELECT o.id, a.track_id, MIN(a.created_at), MAX(a.created_at), COUNT(*)::int
FROM acquisition_intents a
JOIN personal_library_owners o ON o.user_id = a.user_id
WHERE a.track_id IS NOT NULL
  AND a.intent IN ('play', 'queue', 'next')
GROUP BY o.id, a.track_id
ON CONFLICT (owner_id, track_id) DO UPDATE SET
    request_count = personal_library_entries.request_count + EXCLUDED.request_count,
    first_requested_at = LEAST(personal_library_entries.first_requested_at, EXCLUDED.first_requested_at),
    last_requested_at = GREATEST(personal_library_entries.last_requested_at, EXCLUDED.last_requested_at);
