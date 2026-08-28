CREATE TABLE media_holds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('http_stream', 'hmac_stream', 'discord', 'replace', 'acquire')),
    holder_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, holder_id)
);
CREATE INDEX media_holds_track_lease_idx ON media_holds (track_id, lease_until);
CREATE INDEX media_holds_lease_idx ON media_holds (lease_until);

CREATE TABLE managed_cleanup_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL,
    library_id UUID,
    provider_id UUID,
    storage_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'missing', 'skipped_not_managed', 'skipped_in_use')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX managed_cleanup_items_job_idx ON managed_cleanup_items (job_id, status);
