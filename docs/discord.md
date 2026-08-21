# Native Discord

Enable compose profile `discord`. Configure token and application ID under **Admin → Integrations → Discord**. Use **Invite SoundDock Bot** (do not hand-build OAuth URLs).

Playback uses SoundDock search and storage only — no Lavalink, YouTube, or Spotify.

Account linking: `/link` challenge through the web UI; never enter SoundDock passwords in Discord.

Voice PCM is produced by FFmpeg from `StorageProvider` (local path or object stream), not by downloading the public REST API.
