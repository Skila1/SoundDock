-- Isolated workload pools: playback and search keep reserved capacity.

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS pool TEXT NOT NULL DEFAULT 'maintenance';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS priority INT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS result JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE jobs SET pool = CASE
  WHEN type IN ('party.expire', 'radio.refresh') THEN 'playback'
  WHEN type LIKE 'search.%' THEN 'search'
  WHEN type IN ('scapex.fetch', 'ingest.url') THEN 'acquisition'
  WHEN type LIKE 'external.playlist.%' THEN 'sync'
  ELSE 'maintenance'
END
WHERE pool = 'maintenance' OR pool IS NULL OR pool = '';

UPDATE jobs SET priority = CASE pool
  WHEN 'playback' THEN 100
  WHEN 'search' THEN 90
  WHEN 'sync' THEN 50
  WHEN 'acquisition' THEN 40
  ELSE 20
END
WHERE priority = 0;

DROP INDEX IF EXISTS jobs_claim_idx;
CREATE INDEX jobs_claim_idx ON jobs (pool, priority DESC, created_at)
  WHERE status IN ('queued', 'retry');
CREATE INDEX IF NOT EXISTS jobs_pool_status_idx ON jobs (pool, status, updated_at DESC);
