-- Wave 6: durable acquisition intents + replace-source history for Wave 8.
-- Does not drop listen tables.

CREATE TABLE IF NOT EXISTS acquisition_intents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID REFERENCES jobs(id) ON DELETE SET NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id UUID REFERENCES playback_sessions(id) ON DELETE SET NULL,
    track_id UUID REFERENCES tracks(id) ON DELETE SET NULL,
    intent TEXT NOT NULL CHECK (intent IN ('play', 'queue', 'next')),
    source_ref TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'youtube' CHECK (provider IN ('youtube', 'scapex')),
    dest_library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    media_policy_id TEXT NOT NULL DEFAULT 'm4a-0',
    expected_state_revision BIGINT NOT NULL DEFAULT 0,
    expected_instance_id UUID,
    queue_after_item_id UUID,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN (
        'queued', 'downloading', 'processing', 'scanning', 'ready',
        'applied', 'failed', 'cancelled', 'stale'
    )),
    correlation_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS acquisition_intents_coalesce_idx
    ON acquisition_intents (provider, source_ref, dest_library_id, media_policy_id, status);
CREATE INDEX IF NOT EXISTS acquisition_intents_session_idx
    ON acquisition_intents (session_id);
CREATE INDEX IF NOT EXISTS acquisition_intents_job_idx
    ON acquisition_intents (job_id) WHERE job_id IS NOT NULL;

-- One active scapex.fetch (or other coalesced job) per payload coalesce_key.
CREATE UNIQUE INDEX IF NOT EXISTS jobs_coalesce_key_active_uidx
    ON jobs ((payload->>'coalesce_key'))
    WHERE status IN ('queued', 'running', 'retry')
      AND coalesce(payload->>'coalesce_key', '') <> '';

-- Replace-source history. Wave 8 must not add a second migration for this table.
CREATE TABLE IF NOT EXISTS track_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    job_id UUID REFERENCES jobs(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS track_sources_track_idx
    ON track_sources (track_id, acquired_at DESC);
CREATE INDEX IF NOT EXISTS track_sources_ref_idx
    ON track_sources (provider, source_ref);
