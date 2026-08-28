# API

Interactive docs: `/api/docs` (OpenAPI at `/api/v1/openapi.json`).

Bot flow:

1. `GET /api/v1/system/info`
2. Authenticate (Discord OAuth session, or an API key `sd_…` as Bearer)
3. `GET /api/v1/search?q=Linkin+Park+Numb&type=track`
4. Stream with the returned `stream_url` (short-lived token). Never filesystem paths.

Admin Discord invite: `GET /api/v1/admin/integrations/discord/invite`

## Queue SSE

`GET /api/v1/me/queue/sse` (alias `/api/v1/me/queue/events`) is a cookie `EventSource`. The web client opens it with `withCredentials` so the `sd_session` cookie is sent. Do not pass a bearer token as `?access_token=` or `?token=`; those query parameters are rejected.

## Play and media state

Play and queue requests return immediately. They do not block on yt-dlp. Queue items may include `media_state`:

- `ready` - original file is present
- `restoring` - YouTube/ScapeX stub or an open acquisition intent; wait for the job, then play
- `missing_external` - library file missing and there is no managed restore

`GET /api/v1/tracks/{id}/playability` is the browser recovery path. A stream 409 on a managed stub means media is still being restored, not a permanent missing file.

## Stats rebuild

`GET` / `POST /api/v1/admin/stats/rebuild` enqueue the one-time cutover from `listen_history` to `listen_events`. Home, Stats, and Wrapped keep reading history until that job finishes.

## Backups

Encrypted archives. `POST /api/v1/admin/backups` fails if `pg_dump` is missing or no recovery passphrase is set. Restore requires `{confirm: true, passphrase}`. First setup can list/import R2 at `/api/v1/setup/backups/*`. See [backup.md](backup.md).
