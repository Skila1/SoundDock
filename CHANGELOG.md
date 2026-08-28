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
- The web container no longer starts a second Discord gateway. Only `discord-worker` owns voice, so two sessions cannot kick each other out after a few seconds of audio.
- When output is Discord, the web player uses that guild queue (`?target=discord`): pause, resume, seek, and time stay in sync. Resume no longer restarts the track from the beginning.
- Administration can enable or disable an invited Discord server without a second bot token.
- Header search is a dropdown from the top bar, not a modal. It shows two library matches and five YouTube matches. Choosing a result adds it to the queue without skipping the current song. YouTube hits download into the library first.
- Library search requires the real words you typed (title, artist, or album). Weak lookalike matches no longer appear.
- Home shows only the last 15 tracks you actually played, not the rest of the library.
- Spotify playlist import creates SoundDock playlists and downloads missing songs from YouTube via ScapeX. Connect Spotify or paste a playlist URL.
- Autoplay is off unless you turn it on. It seeds from recent listening, skips the current queue and a recent-history window, and only relaxes those exclusions if the similar-track pool is too small. YouTube is a fallback, not a duplicate mill.
- The user sidebar is Home, Search, Library, Playlists, Radio, and Connected Services. Library holds Tracks, Albums, Artists, and Favourites, plus Add music and Import. History, stats, Wrapped, and Party sit in a small Listening group. Administration appears in the sidebar only for admins. Account, devices, help, and Discord live in the profile menu.
- The queue is a collapsible drawer opened from the player. It shows Now playing and Up next, with History behind a header control. Queued tracks can be removed individually. Clear only drops Up next.
- Administration is grouped into System, Access, and Media. Groups (RBAC) assign SoundDock permissions; Discord role links are optional membership mapping and never override local permissions.
- Admins can rename, delete, merge, and set a default library. Catalogue delete does not touch NAS or local source files. Managed-file deletion is a separate confirmation.
- Tracks can be bulk-deleted or cleared. Spotify playlists keep their Spotify id and can be synced again; missing songs still come through YouTube inside SoundDock.
- YouTube search and fetch run inside SoundDock. There is no separate ScapeX service.
- Administration > System > Workers exposes playback, search, acquisition, sync, and maintenance pools with reserved capacity for playback and search. Hung yt-dlp, Spotify imports, scans, merges, deletes, and metadata jobs are queued and cannot starve the API.
- Clear queue keeps the song that is playing. Play on a track while something is already playing adds it to the queue instead of replacing the current song.
- Help and Discord server buttons open the SoundDock community invite.

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
