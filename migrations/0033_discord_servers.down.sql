DROP INDEX IF EXISTS operational_logs_guild_idx;
DROP INDEX IF EXISTS audit_events_guild_idx;
ALTER TABLE discord_guilds
  DROP CONSTRAINT IF EXISTS discord_guilds_queue_mode_check,
  DROP CONSTRAINT IF EXISTS discord_guilds_library_mode_check,
  DROP COLUMN IF EXISTS owner_id,
  DROP COLUMN IF EXISTS owner_name,
  DROP COLUMN IF EXISTS member_count,
  DROP COLUMN IF EXISTS bot_permissions,
  DROP COLUMN IF EXISTS info_refreshed_at,
  DROP COLUMN IF EXISTS joined_at,
  DROP COLUMN IF EXISTS left_at,
  DROP COLUMN IF EXISTS queue_mode,
  DROP COLUMN IF EXISTS library_mode,
  DROP COLUMN IF EXISTS banned_at,
  DROP COLUMN IF EXISTS banned_by,
  DROP COLUMN IF EXISTS ban_reason;
