# SoundDock

Self-hosted music library and streaming platform. Users supply their own media. SoundDock does not provide copyrighted catalogues, YouTube/Spotify ripping, or DRM circumvention.

License: **GNU Affero General Public License v3.0 or later**.

## Features

- Libraries on local disk, NAS/NFS/SMB mounts, Docker volumes, and S3-compatible object storage (Cloudflare R2, AWS S3, MinIO, B2)
- Scanning, Remote Import (SSRF-safe HTTP(S) URLs), resumable uploads, migrate-into-managed-storage
- Search API for humans and bots
- Web/PWA player with queue, ReplayGain, optional crossfade
- Optional native Discord worker (no Lavalink/YouTube/Spotify)
- REST API, OpenAPI at `/api/docs`, optional OpenSubsonic, webhooks
- PostgreSQL system of record; Redis and Meilisearch optional

## Quick start (no git clone)

```bash
curl -fsSL https://raw.githubusercontent.com/sounddock/sounddock/main/install.sh | sudo bash
```

The installer installs Docker if needed, writes `/opt/sounddock/docker-compose.yml` + `.env`, **pulls the published image**, and starts SoundDock. Discord sign-in is optional. If you enable it, set the OAuth redirect to `https://your-host/api/v1/auth/discord/callback` and optionally `SD_ADMIN_DISCORD_ID`. If you skip Discord, create the first administrator in the web UI.

Control: `sudo bash /opt/sounddock/../install.sh` is not needed; keep a copy of `install.sh` or re-download:

```bash
sudo bash install.sh status|update|logs|uninstall|doctor
```

## Docker (this repo / development)

```bash
cp .env.example .env
# set POSTGRES_PASSWORD, SD_MASTER_KEY
# optional Discord: SD_DISCORD_ENABLED=true, SD_DISCORD_CLIENT_ID, SD_DISCORD_CLIENT_SECRET, SD_ADMIN_DISCORD_ID
docker compose up -d --build
```

Production hosts should `docker compose pull && docker compose up -d` so they use `ghcr.io/sounddock/sounddock:latest` instead of cloning.

Discord bot worker: `docker compose --profile discord up -d`  
Cloudflare Tunnel: `docker compose --profile cloudflare up -d`

## Development (Go)

```bash
# PostgreSQL 16
docker run -d --name sd-pg -e POSTGRES_PASSWORD=sounddock -e POSTGRES_USER=sounddock -e POSTGRES_DB=sounddock -p 5432:5432 postgres:16-alpine

export SD_DATABASE_URL='postgres://sounddock:sounddock@127.0.0.1:5432/sounddock?sslmode=disable'
export SD_MASTER_KEY='dev-master-key-change-me'
export SD_DISCORD_ENABLED=true
export SD_DISCORD_CLIENT_ID=...
export SD_DISCORD_CLIENT_SECRET=...
export SD_ADMIN_DISCORD_ID=...
cd web && npm install && npm run build && cd ..
go run ./cmd/sounddock all
```

Open http://localhost:8080. With Discord on, sign in with Discord. With Discord off, create the first administrator.

## Documentation

- [Installation](docs/install.md)
- [Architecture](docs/architecture.md)
- [API](docs/api.md)
- [Docker](docs/docker.md)
- [Environment](docs/environment.md)
- [Security](SECURITY.md)
- [Discord](docs/discord.md)

## Legal

SoundDock organises and streams **user-supplied** media only.
