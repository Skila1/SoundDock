CREATE TABLE operational_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    level TEXT NOT NULL DEFAULT 'info' CHECK (level IN ('debug', 'info', 'warn', 'error')),
    category TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    actor_id UUID,
    job_id UUID,
    library_id UUID,
    track_id UUID,
    details JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX operational_logs_created_idx ON operational_logs (created_at DESC);
CREATE INDEX operational_logs_category_idx ON operational_logs (category, created_at DESC);
CREATE INDEX operational_logs_level_idx ON operational_logs (level, created_at DESC);
