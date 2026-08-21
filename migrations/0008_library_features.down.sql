-- Conservative down: refuse if Wave 1 data exists.

DO $$
DECLARE
    busy BOOLEAN;
BEGIN
    SELECT
        EXISTS (SELECT 1 FROM play_counts WHERE skip_count <> 0)
        OR EXISTS (SELECT 1 FROM track_files WHERE deleted_at IS NOT NULL)
        OR EXISTS (SELECT 1 FROM playlist_snapshots)
        OR EXISTS (SELECT 1 FROM party_members)
        OR EXISTS (SELECT 1 FROM party_votes)
        OR EXISTS (SELECT 1 FROM smart_playlist_rules)
        OR EXISTS (SELECT 1 FROM playback_sessions WHERE party_enabled OR device_id IS NOT NULL)
    INTO busy;

    IF busy THEN
        RAISE NOTICE '0008 down no-op: listen/device/snapshot/trash/party data is present';
        RETURN;
    END IF;

    DELETE FROM server_settings WHERE key IN (
        'announcement', 'maintenance', 'stream_remote_max_bitrate', 'stream_lan_max_bitrate',
        'stream_remote_concurrency', 'hires_bit_depth_min', 'hires_sample_rate_min', 'compression_preset'
    );

    DROP TABLE IF EXISTS smart_playlist_rules;
    DROP TABLE IF EXISTS playlist_snapshots;
    DROP TABLE IF EXISTS party_votes;
    DROP TABLE IF EXISTS party_members;

    ALTER TABLE track_files
        DROP COLUMN IF EXISTS original_size_bytes,
        DROP COLUMN IF EXISTS deleted_at;

    ALTER TABLE tracks
        DROP COLUMN IF EXISTS metadata_source,
        DROP COLUMN IF EXISTS metadata_confidence,
        DROP COLUMN IF EXISTS manual_gain_db,
        DROP COLUMN IF EXISTS waveform_peaks;

    ALTER TABLE play_counts DROP COLUMN IF EXISTS skip_count;

    ALTER TABLE playback_sessions
        DROP COLUMN IF EXISTS device_id,
        DROP COLUMN IF EXISTS shuffle_mode,
        DROP COLUMN IF EXISTS stop_after_current,
        DROP COLUMN IF EXISTS last_device,
        DROP COLUMN IF EXISTS party_enabled,
        DROP COLUMN IF EXISTS party_expires_at,
        DROP COLUMN IF EXISTS party_host_user_id;
END $$;
