-- Activity log: request/correlation context on operational_logs so the admin
-- Activity view can answer who did what, from where, and whether it worked.
ALTER TABLE operational_logs
    ADD COLUMN IF NOT EXISTS request_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ip TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS actor_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS action TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS method TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS route TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status INT,
    ADD COLUMN IF NOT EXISTS duration_ms INT,
    ADD COLUMN IF NOT EXISTS result TEXT NOT NULL DEFAULT '';

ALTER TABLE operational_logs DROP CONSTRAINT IF EXISTS operational_logs_result_check;
ALTER TABLE operational_logs ADD CONSTRAINT operational_logs_result_check CHECK (result IN ('', 'success', 'failure'));

CREATE INDEX IF NOT EXISTS operational_logs_actor_idx ON operational_logs (actor_id, created_at DESC) WHERE actor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS operational_logs_ip_idx ON operational_logs (ip, created_at DESC) WHERE ip <> '';
CREATE INDEX IF NOT EXISTS operational_logs_request_idx ON operational_logs (request_id) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS operational_logs_result_idx ON operational_logs (result, created_at DESC) WHERE result <> '';

ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS request_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS audit_events_actor_idx ON audit_events (actor_user_id, created_at DESC);
