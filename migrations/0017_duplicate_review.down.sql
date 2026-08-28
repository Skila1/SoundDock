-- Reverse Wave 8b duplicate-review objects only.

DROP INDEX IF EXISTS duplicate_review_groups_open_idx;
DROP INDEX IF EXISTS duplicate_review_groups_group_uidx;
DROP TABLE IF EXISTS duplicate_review_groups;
DROP INDEX IF EXISTS duplicate_groups_blocking_key_uidx;
ALTER TABLE duplicate_groups DROP COLUMN IF EXISTS blocking_key;
