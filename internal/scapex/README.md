# YouTube fetch (formerly ScapeX)

YouTube search and download run inside the SoundDock process. SoundDock writes audio to `managed/inbox` and enqueues a library scan. Play and queue HTTP do not wait for yt-dlp: they enqueue acquisition and return tracks with `media_state` `restoring`.

`SD_SCAPEX_URL` is **deprecated**. Leave it empty. If set, SoundDock still talks to that leftover HTTP sidecar. The sidecar is not in Compose.

Age-gated videos: `SCAPEX_YT_COOKIES` or `SCAPEX_YT_BROWSER`.
