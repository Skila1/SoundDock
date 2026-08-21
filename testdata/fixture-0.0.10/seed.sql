-- Deterministic 0.0.10 fixture. Apply after migrations 0001-0007, before 0008.
-- IDs are stable so Wave 0 upgrade tests and Wave 3 share the same rows.

-- User (web_device owner_key is this UUID)
INSERT INTO users (id, username, password_hash, display_name)
VALUES (
  '00000000-0000-4000-8000-000000000001',
  'fixture',
  '$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ01234',
  'Fixture User'
);

INSERT INTO user_roles (user_id, role_id)
SELECT '00000000-0000-4000-8000-000000000001', id FROM roles WHERE name = 'Administrator';

INSERT INTO user_identities (user_id, provider, provider_user_id, provider_username)
VALUES (
  '00000000-0000-4000-8000-000000000001',
  'discord',
  '288559247741157386',
  'fixture'
);

INSERT INTO storage_providers (id, name, type)
VALUES ('00000000-0000-4000-8000-000000000010', 'local', 'local');

INSERT INTO libraries (
  id, name, kind, storage_provider_id, root_prefix,
  organisation_mode, allow_physical_reorganise, read_only
) VALUES (
  '00000000-0000-4000-8000-000000000020',
  'Music',
  'music',
  '00000000-0000-4000-8000-000000000010',
  '',
  'virtual',
  FALSE,
  FALSE
);

INSERT INTO library_grants (library_id, user_id, actions)
VALUES (
  '00000000-0000-4000-8000-000000000020',
  '00000000-0000-4000-8000-000000000001',
  ARRAY['read','stream','upload']
);

INSERT INTO artists (id, name, sort_name)
VALUES (
  '00000000-0000-4000-8000-000000000030',
  'Linkin Park',
  'Linkin Park'
);

INSERT INTO albums (id, title, year, library_id)
VALUES (
  '00000000-0000-4000-8000-000000000040',
  'Meteora',
  2003,
  '00000000-0000-4000-8000-000000000020'
);

INSERT INTO album_artists (album_id, artist_id, role, position)
VALUES (
  '00000000-0000-4000-8000-000000000040',
  '00000000-0000-4000-8000-000000000030',
  'album_artist',
  0
);

-- Tagged track (ID3 title wins)
INSERT INTO tracks (id, library_id, album_id, title, duration_ms, track_number, disc_number)
VALUES (
  '00000000-0000-4000-8000-000000000050',
  '00000000-0000-4000-8000-000000000020',
  '00000000-0000-4000-8000-000000000040',
  'Numb',
  185000,
  13,
  1
);

-- Filename-parsed track (no hash title)
INSERT INTO tracks (id, library_id, album_id, title, duration_ms, track_number, disc_number)
VALUES (
  '00000000-0000-4000-8000-000000000051',
  '00000000-0000-4000-8000-000000000020',
  '00000000-0000-4000-8000-000000000040',
  'In The End',
  216000,
  8,
  1
);

INSERT INTO track_artists (track_id, artist_id, role, position) VALUES
  ('00000000-0000-4000-8000-000000000050', '00000000-0000-4000-8000-000000000030', 'primary', 0),
  ('00000000-0000-4000-8000-000000000051', '00000000-0000-4000-8000-000000000030', 'primary', 0);

-- Hash storage keys. quality must remain original.
INSERT INTO track_files (
  id, track_id, library_id, storage_key, size_bytes, content_hash, codec, container, quality
) VALUES (
  '00000000-0000-4000-8000-000000000060',
  '00000000-0000-4000-8000-000000000050',
  '00000000-0000-4000-8000-000000000020',
  'uploads/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.flac',
  1234567,
  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'flac',
  'flac',
  'original'
), (
  '00000000-0000-4000-8000-000000000061',
  '00000000-0000-4000-8000-000000000051',
  '00000000-0000-4000-8000-000000000020',
  'uploads/bb/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.mp3',
  2345678,
  'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
  'mpeg',
  'mp3',
  'original'
);

INSERT INTO playback_sessions (
  id, kind, owner_key, user_id, volume, repeat_mode, shuffle,
  current_index, current_track_id, position_ms, status
) VALUES (
  '00000000-0000-4000-8000-000000000070',
  'web_device',
  '00000000-0000-4000-8000-000000000001',
  '00000000-0000-4000-8000-000000000001',
  1,
  'off',
  FALSE,
  0,
  '00000000-0000-4000-8000-000000000050',
  12000,
  'paused'
), (
  '00000000-0000-4000-8000-000000000071',
  'discord_guild',
  '111111111111111111',
  NULL,
  1,
  'off',
  FALSE,
  0,
  '00000000-0000-4000-8000-000000000050',
  0,
  'stopped'
);

INSERT INTO playback_queue_items (id, session_id, position, track_id) VALUES
  ('00000000-0000-4000-8000-000000000080', '00000000-0000-4000-8000-000000000070', 0, '00000000-0000-4000-8000-000000000050'),
  ('00000000-0000-4000-8000-000000000081', '00000000-0000-4000-8000-000000000070', 1, '00000000-0000-4000-8000-000000000051'),
  ('00000000-0000-4000-8000-000000000082', '00000000-0000-4000-8000-000000000071', 0, '00000000-0000-4000-8000-000000000050');

INSERT INTO discord_guilds (id, name, enabled)
VALUES ('111111111111111111', 'Fixture Guild', TRUE);

INSERT INTO discord_voice_runtime (guild_id, voice_channel_id, session_id, connected)
VALUES ('111111111111111111', NULL, '00000000-0000-4000-8000-000000000071', FALSE);
