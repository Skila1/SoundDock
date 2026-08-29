ALTER TABLE discord_user_voice
    ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
