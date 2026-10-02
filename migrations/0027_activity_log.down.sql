DROP INDEX IF EXISTS audit_events_actor_idx;
ALTER TABLE audit_events DROP COLUMN IF EXISTS request_id;

DROP INDEX IF EXISTS operational_logs_result_idx;
DROP INDEX IF EXISTS operational_logs_request_idx;
DROP INDEX IF EXISTS operational_logs_ip_idx;
DROP INDEX IF EXISTS operational_logs_actor_idx;
ALTER TABLE operational_logs DROP CONSTRAINT IF EXISTS operational_logs_result_check;
ALTER TABLE operational_logs
    DROP COLUMN IF EXISTS result,
    DROP COLUMN IF EXISTS duration_ms,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS route,
    DROP COLUMN IF EXISTS method,
    DROP COLUMN IF EXISTS action,
    DROP COLUMN IF EXISTS actor_name,
    DROP COLUMN IF EXISTS ip,
    DROP COLUMN IF EXISTS request_id;
