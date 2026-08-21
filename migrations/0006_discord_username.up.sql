-- Replace leftover discord_<id> local usernames with the Discord username, or the raw Discord id.

UPDATE users u
SET username = i.provider_username,
    updated_at = now()
FROM user_identities i
WHERE i.user_id = u.id
  AND i.provider = 'discord'
  AND u.username = 'discord_' || i.provider_user_id
  AND coalesce(i.provider_username, '') <> ''
  AND NOT EXISTS (
    SELECT 1 FROM users x
    WHERE lower(x.username) = lower(i.provider_username) AND x.id <> u.id
  );

UPDATE users u
SET username = i.provider_user_id,
    updated_at = now()
FROM user_identities i
WHERE i.user_id = u.id
  AND i.provider = 'discord'
  AND u.username = 'discord_' || i.provider_user_id;
