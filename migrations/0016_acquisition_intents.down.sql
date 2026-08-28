-- Reverse Wave 6 acquisition objects only. Do not drop listen tables.

DROP TABLE IF EXISTS track_sources;
DROP TABLE IF EXISTS acquisition_intents;
DROP INDEX IF EXISTS jobs_coalesce_key_active_uidx;
