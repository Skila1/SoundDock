-- Reverse Wave 4 shadow tables. Do not drop listen_history.

DELETE FROM retention_policies WHERE key = 'listen_events';

DROP TABLE IF EXISTS listen_output_segments;
DROP TABLE IF EXISTS listen_instance_state;
DROP TABLE IF EXISTS listen_events;
