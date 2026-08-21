-- Discord registration whitelist (server and/or role). Both gates are independently toggleable.

ALTER TABLE discord_settings
    ADD COLUMN IF NOT EXISTS registration_guild_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS registration_guild_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS registration_role_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS registration_role_id TEXT NOT NULL DEFAULT '';
