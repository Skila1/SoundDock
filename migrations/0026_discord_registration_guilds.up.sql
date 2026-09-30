-- Registration whitelist: any number of Discord servers, each with optional role IDs.
-- A new Discord user may register when they are in any listed server and, if that
-- server lists roles, hold at least one of them there.
-- discord_settings.registration_guild_enabled is now the single whitelist switch.

CREATE TABLE IF NOT EXISTS discord_registration_guilds (
    guild_id TEXT PRIMARY KEY,
    label TEXT NOT NULL DEFAULT '',
    role_ids TEXT[] NOT NULL DEFAULT '{}',
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO discord_registration_guilds (guild_id, role_ids)
SELECT trim(registration_guild_id),
       CASE WHEN registration_role_enabled AND trim(registration_role_id) <> ''
            THEN ARRAY[trim(registration_role_id)]
            ELSE '{}'::TEXT[] END
FROM discord_settings
WHERE id = 1 AND trim(registration_guild_id) <> ''
ON CONFLICT (guild_id) DO NOTHING;

UPDATE discord_settings
SET registration_guild_enabled = registration_guild_enabled OR registration_role_enabled
WHERE id = 1;
