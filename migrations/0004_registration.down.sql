ALTER TABLE discord_settings
    DROP COLUMN IF EXISTS registration_guild_enabled,
    DROP COLUMN IF EXISTS registration_guild_id,
    DROP COLUMN IF EXISTS registration_role_enabled,
    DROP COLUMN IF EXISTS registration_role_id;
