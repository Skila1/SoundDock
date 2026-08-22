# Changelog

## 0.1.0

- Queue, listen history, Discord voice as the listening surface when enabled, radio, Wrapped, playlists, offline stream tokens, and admin health/maintenance.
- Playback, scrobble, and `/healthz` stay available during maintenance. Remote streams fail closed to the LAN/remote policy.
- Library titles come from ID3 tags or the original upload name, never the hashed storage filename.
- Playlist numbers like `321.` are stripped. Scanning also retitles tracks that already stored a hash as the title, and updates their artist and album.
- When tags are missing, `Artist - Title` and `Title - Artist` are both understood: tags first, then artists already in the library, then `Artist - Title` as the default.
- Hover the player volume control and scroll the mouse wheel to raise or lower volume.
- In-app Update now writes the host request immediately, then pulls via the Docker socket if systemd inotify missed the bind-mount write.
- API keys are created in Administration with explicit scopes. Profile no longer mints keys.
- Discord voice join waits until the connection is ready and tears down failed sessions so the bot does not keep leaving and rejoining.
- `docker compose up -d` now starts `discord-worker` by default. The old `discord` profile is no longer required.
- Discord voice uses a DAVE/E2EE-capable discordgo fork. Discord now rejects clients that omit `max_dave_protocol_version` with close 4017, which looked like the bot joining and leaving.
- Discord join/play reuses a healthy voice session instead of disconnecting first. A kicked bot stays left until someone asks it to join again.
- Discord voice no longer skips most Opus frames on each Ogg page, which made tracks sound several times too fast.

## 0.0.9

- Use artist and title from `321. Artist - Song.mp3` names when tags are missing. Playlist numbers are not kept as the track title.

## 0.0.8

- Updates page shows the new version and changelog before you install.
- Update now only signals the host helper, which runs `docker compose pull` then `docker compose up -d`.
- A progress bar tracks the image pull. SoundDock stays up until the new container starts. Postgres is not recreated.
- WAV and AIFF uploads are stored as FLAC. Files that are already compressed are left alone.
- Bulk upload runs up to 100 files at a time.

## 0.0.7

- Index uploaded audio as soon as it lands, including zip archives.
- Scan after upload no longer fails on a missing job id, so Home and Tracks can show large libraries.

## 0.0.6

- Stop the Updates page sticking on Updating when a host request file is left behind.

## 0.0.5

- Bulk file, folder, and zip uploads plus multi-URL remote import.

## 0.0.4

- Upload and URL import work without picking a library first, and only accept audio files.

## 0.0.3

- Fix admin user ids and Discord usernames, and apply in-app updates without recreating Postgres.

## 0.0.1

- User management, host systemd updates, and the first numbered release.
