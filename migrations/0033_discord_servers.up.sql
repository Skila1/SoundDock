-- Discord server management: owner and bot details, per-server queue and
-- library scope, and a ban list the bot leaves on sight.
ALTER TABLE discord_guilds
  ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS owner_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS member_count INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS bot_permissions BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS info_refreshed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS joined_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS left_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS queue_mode TEXT NOT NULL DEFAULT 'personal',
  ADD COLUMN IF NOT EXISTS library_mode TEXT NOT NULL DEFAULT 'all',
  ADD COLUMN IF NOT EXISTS banned_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS banned_by UUID REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS ban_reason TEXT NOT NULL DEFAULT '';

-- Servers that exist today keep personal queues; new servers get one shared queue.
ALTER TABLE discord_guilds ALTER COLUMN queue_mode SET DEFAULT 'shared';

ALTER TABLE discord_guilds
  ADD CONSTRAINT discord_guilds_queue_mode_check CHECK (queue_mode IN ('shared', 'personal')),
  ADD CONSTRAINT discord_guilds_library_mode_check CHECK (library_mode IN ('all', 'selected'));

-- Per-server activity log on the Discord page.
CREATE INDEX IF NOT EXISTS operational_logs_guild_idx
  ON operational_logs ((details->>'guild_id'), created_at DESC) WHERE details ? 'guild_id';
CREATE INDEX IF NOT EXISTS audit_events_guild_idx
  ON audit_events ((meta->>'guild_id'), created_at DESC) WHERE meta ? 'guild_id';
