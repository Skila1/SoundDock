-- Discord sign-in is configured in Admin, not env. First Discord user can be recorded as admin.

ALTER TABLE discord_settings
    ADD COLUMN IF NOT EXISTS login_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS admin_discord_ids TEXT NOT NULL DEFAULT '';
