ALTER TABLE discord_settings
    DROP COLUMN IF EXISTS login_enabled,
    DROP COLUMN IF EXISTS admin_discord_ids;
