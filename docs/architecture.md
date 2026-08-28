# Architecture

SoundDock is a Go modular monolith:

- `api` / `worker` / `all`: HTTP + River-style Postgres jobs (`SKIP LOCKED`)
- `discord`: optional first-party Discord worker, same image (`discord-worker` in the default Compose stack)
- PostgreSQL is the system of record
- Redis and Meilisearch are optional compose profiles

Clients: Web/PWA, REST, native Discord, Vocard (via REST). Discord must not be imported by core packages. Web player shortcuts stay off until enabled (`sd-prefs`); Ctrl+K / Cmd+K still focuses header search.

Storage: `StorageProvider` for local, S3/R2, and managed paths. Virtual organisation indexes in place; managed organisation may rename only owned writable libraries.

## Playback session

A user has one logical playback session. Browser and Discord bind to that session; they do not copy the queue. Switching output (Browser / Discord) changes the renderer, not the queue contents. The web player does not select a second queue with `?target=discord`.

Live updates: `GET /api/v1/me/queue/sse` (alias `/api/v1/me/queue/events`) is a cookie `EventSource` (`withCredentials`). Query `access_token` / `token` is rejected.

Play and queue HTTP do not wait on yt-dlp. YouTube-shaped refs enqueue acquisition and return with `media_state` `restoring` (or `missing_external` when the file is gone and there is nothing to restore). The stream endpoint does not start a fetch.

## Library grants

`library_grants` is a per-library ACL. Rows grant `read`, `stream`, and/or `write` on one library to a user or a role. They are not global groups. Keep the table.

## Listen stats

`listen_history` is the production recap source until an administrator runs **Admin → Media → Stats rebuild**. That job rebuilds `play_counts` from `listen_events` and flips the reader to `listen_events`. The two tables are not merged into one total. Recap minutes that coalesce null `listened_ms` to track duration are estimated.

## Workers

**Admin → System → Workers** exposes job pools. `max_rss_mb` (UI: Memory cap, advisory) is a stored hint. It is not a cgroup or an enforced memory limit. Concurrency is capped by pool min/max workers.

## Metadata

Scan-side Cover Art Archive lookups run when **Admin → Media → Metadata** external providers are on, the file has a MusicBrainz release MBID, and there is no embedded or folder art. This is not a Discord or playback path.

## YouTube fetch

YouTube search and download run inside the SoundDock process. `SD_SCAPEX_URL` is deprecated leftover sidecar support; leave it empty. Compose does not ship a ScapeX service.

Query review notes: [`docs/query-baselines.md`](query-baselines.md). Manual device checks: [`docs/device-matrix.md`](device-matrix.md).

See the repository `internal/` layout.
