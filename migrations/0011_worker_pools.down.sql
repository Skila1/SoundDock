DROP INDEX IF EXISTS jobs_pool_status_idx;
DROP INDEX IF EXISTS jobs_claim_idx;
CREATE INDEX jobs_claim_idx ON jobs (status, run_after) WHERE status IN ('queued', 'retry');
ALTER TABLE jobs DROP COLUMN IF EXISTS result;
ALTER TABLE jobs DROP COLUMN IF EXISTS finished_at;
ALTER TABLE jobs DROP COLUMN IF EXISTS started_at;
ALTER TABLE jobs DROP COLUMN IF EXISTS priority;
ALTER TABLE jobs DROP COLUMN IF EXISTS pool;
