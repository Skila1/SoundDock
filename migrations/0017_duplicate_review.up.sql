-- Wave 8b: bounded duplicate review rows (member track ids, not N×N pairs).
-- duplicate_groups already exists; blocking_key makes groups stable across scans.

ALTER TABLE duplicate_groups
    ADD COLUMN IF NOT EXISTS blocking_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS duplicate_groups_blocking_key_uidx
    ON duplicate_groups (blocking_key)
    WHERE blocking_key IS NOT NULL AND blocking_key <> '';

CREATE TABLE IF NOT EXISTS duplicate_review_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID REFERENCES duplicate_groups(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'merged', 'ignored')),
    reason TEXT NOT NULL DEFAULT '',
    track_ids UUID[] NOT NULL DEFAULT ARRAY[]::UUID[],
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS duplicate_review_groups_group_uidx
    ON duplicate_review_groups (group_id)
    WHERE group_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS duplicate_review_groups_open_idx
    ON duplicate_review_groups (created_at DESC)
    WHERE status = 'open';
