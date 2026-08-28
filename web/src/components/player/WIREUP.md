# Web player session wiring

Authoritative playback is the attached `playback_sessions` row. Browser and Discord are renderers that bind to it. There is no unfinished PUT-replace TODO.

## Queue replace (`PUT /api/v1/me/queue`)

`playTracks` uses PUT replace when the player is **not** already live (paused click-append and “add while playing” go through `POST /me/queue/add`).

The body includes `command_id` (client UUID). The server runs action `replace` through the command-receipt path: same id + hash replays the stored result; a hash mismatch is `409 command_conflict`.

Play now (jump to an existing index) is `POST /me/queue/control` `action=index`, never PUT.

## Other routes

- `GET /api/v1/me/queue` — attached session snapshot (`state_revision`, playhead, `renderer_id`, `binding_revision`, listeners)
- `POST /api/v1/me/queue/control` — mutating controls; `command_id` required from the web client
- `GET /api/v1/tracks/{id}/stream` — cookie or HMAC; library `stream` grant when a user is present
- `GET /api/v1/radio?fill=youtube` — YouTube fill only when an audio listener exists (browser lease playing, or Discord lease + human in bound VC)
- Discord: `POST /me/discord/join` → `BindDiscordRenderer`; switch back to browser keeps the bind

`?target=discord` is not a second-queue selector.
