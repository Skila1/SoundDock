ALTER TABLE user_identities
    ADD COLUMN IF NOT EXISTS avatar_hash TEXT;
