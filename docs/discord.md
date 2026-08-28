# Native Discord

The `discord-worker` service is in the default Compose stack (same image, `sounddock discord`). Configure token and application ID under **Admin → Integrations → Discord**. Use **Invite SoundDock Bot** (do not hand-build OAuth URLs).

Playback uses SoundDock search and storage only. No Lavalink, YouTube, or Spotify.

The native Discord worker is a first-party client of the same API as the web player. Browser and Discord bind one logical session; the queue is not copied.

Account linking: Discord `/link` issues a challenge completed in the web UI (Connected Services / profile). Never enter SoundDock passwords in Discord. Optional Discord OAuth sign-in is configured on the same Admin Discord page.

Voice PCM is produced by FFmpeg from `StorageProvider` (local path or object stream), not by downloading the public REST API.
