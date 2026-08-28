# YouTube fetch (formerly ScapeX)

YouTube search and download run inside the SoundDock process. SoundDock writes audio to `managed/inbox`, enqueues a library scan, and waits for the new track ids.

`SD_SCAPEX_URL` is optional leftover support for an old sidecar. Leave it empty.

Age-gated videos: `SCAPEX_YT_COOKIES` or `SCAPEX_YT_BROWSER`.
