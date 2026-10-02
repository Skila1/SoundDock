# Changelog

## 0.1.2

- The admin panel is reorganised into 12 pages: Dashboard, Activity, Backups, Updates, Integrations, Users, Groups, Discord, Libraries & Storage, Catalog, Media Settings, and Retention. Old admin links redirect to the matching page.
- New Activity page with Live, Jobs & Workers, Logs, Audit, and Errors tabs. It records who did what, when, from which IP, and whether it worked, with search, filters, and request IDs.
- Upload and library limits now live on the Users and Libraries pages. Library access is a tab on Libraries & Storage.
- Passwords, tokens, keys, and other secrets are never stored in the activity log.

## 0.1.1

- The site is installable as a phone app (Add to Home Screen on iOS, Install on Android) with proper icons and standalone display.
- The mobile player no longer paints through queues, lyrics, or dialogs.
- Lock screen and CarPlay keep playing after you lock the phone, and the ±10 second buttons are replaced with previous and next track.

## 0.1.0

- The progress bar keeps up when playing through Discord. A track change from the Discord worker no longer leaves the bar on the previous track's position, and the new track's position updates are no longer discarded as stale for its first minute or so.
- Live updates reach every web client across processes: skip, pause, seek, Discord bind and unbind, renderer switches and party changes all publish, and a newly connected tab gets current state immediately.
- The web client reconnects a live stream that went quiet without an error (proxy, sleep, network change) and resyncs when the network comes back. Returning to the tab refetches stale data.
- Uploads, imports, scans, YouTube fetches, metadata edits, deletes and playlist changes refresh open pages for everyone without a reload.
- A seek that never gets an answer no longer freezes the progress bar. Playback controls time out after 10 seconds.
- My Library only lists songs you picked yourself: a single song played or queued, Discord `/play` for one song, or a YouTube request. Playing or queueing a whole album, playlist or radio station no longer adds every song, and autoplay never does. Select songs to remove them, or clear My Library.
- Album, artist and track pages have a Change cover button to upload your own image or go back to the embedded one. Anyone with write access to the library can change covers. An uploaded cover now wins over embedded art, including after a rescan and in track lists and the player, and new covers show without a reload.
- Track lists have checkboxes and Select all. Administrators can bulk delete any selection, see how many songs were skipped, and delete a whole album from its page. Albums and artists left empty by a delete are removed.
- The Discord registration whitelist accepts any number of servers, each with optional role IDs. A new Discord account may register if it is in any listed server and, when that server lists roles, holds at least one of them there. Existing single server and role settings migrate automatically (migration 0026).
- Saving Discord sign-in settings no longer switches the Discord bot off.
- Joining or playing on Discord no longer fails with `bind_conflict` after the bot disconnected, was kicked, or you last used a different server. A join you start replaces a stale voice binding.
- A signed-out visit no longer requests `/api/v1/me` twice.
- Player keyboard shortcuts (space, arrows, n/p/m) are off until enabled. The flag is stored in browser `sd-prefs` and defaults to false. Ctrl+K / Cmd+K still opens header search.
- Listen recap (Home, Stats, Wrapped) reads `listen_history` until an admin runs **Stats rebuild**, which rebuilds `play_counts` from `listen_events` and flips the reader. Recap minutes that fill null `listened_ms` from track duration are estimated. The two tables are not a merged listen total.
- `SD_SCAPEX_URL` is deprecated. Leave it empty so YouTube search/fetch runs in-process. A leftover sidecar still works if the URL is set; Compose does not include one.
- `library_grants` remains the per-library ACL (`read` / `stream` / `write` on a library for a user or role). It is not global groups. Do not drop the table.
- Queue live updates use a cookie `EventSource` (`/api/v1/me/queue/sse`). Query bearer tokens are rejected.
- Play HTTP does not wait for yt-dlp. YouTube enqueue returns `media_state` `restoring` until the file is ready.
- Browser and Discord bind one logical playback session. The queue is not copied. The web player does not use `?target=discord`.
- Worker pool **Memory cap (MB, advisory)** is a stored hint, not a cgroup limit.
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
- When output is Discord, pause, resume, seek, and time stay in sync on the bound session. Resume no longer restarts the track from the beginning.
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
- Play starts that track when nothing is playing. Play on another track while one is already playing adds it once, and a double-click cannot enqueue the same song twice.
- Administration > Media > Retention can prune ScapeX / YouTube-acquired tracks by age, managed storage, or free disk space. Favourites, Keep forever, manual playlists, the queue, and NAS libraries stay unless an admin opts a library in. Pruned playlist tracks remain as re-acquirable stubs.

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
