# Architecture

SoundDock is a Go modular monolith:

- `api` / `worker` / `all` — HTTP + River-style Postgres jobs (`SKIP LOCKED`)
- `discord` — optional first-party Discord worker, same image
- PostgreSQL is the system of record
- Redis and Meilisearch are optional compose profiles

Clients: Web/PWA, REST, native Discord, Vocard (via REST). Discord must not be imported by core packages.

Storage: `StorageProvider` for local, S3/R2, and managed paths. Virtual organisation indexes in place; managed organisation may rename only owned writable libraries.

See the repository `internal/` layout.
