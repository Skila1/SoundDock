# SoundDock

**Your music. Your server. Your way.**

SoundDock is an open-source, self-hosted music platform for organising, streaming and integrating your own music library.

```bash
export SD_IMAGE=ghcr.io/skila1/sounddock:latest
curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh | sudo bash
```

That installs Docker if needed, writes `/opt/sounddock`, pulls the image, and starts SoundDock. Discord sign-in is optional. If you skip it, create the first administrator in the browser.

```bash
sudo bash install.sh status|update|logs|uninstall|doctor
```

You bring the files. SoundDock does not provide catalogues, rip YouTube or Spotify, or bypass DRM.

License: **GNU Affero General Public License v3.0 or later**.

## Music infrastructure

SoundDock is built to be the system your library, players, and bots talk to. The web player and native Discord worker are first-party clients of the same API.

- **Storage.** Libraries on local disk, NAS/NFS/SMB, Docker volumes, and S3-compatible object storage (Cloudflare R2, AWS S3, MinIO, B2). Scan in place, resumable uploads, Remote Import of direct HTTP(S) media URLs, migrate into managed storage.
- **API.** REST `/api/v1`, OpenAPI at `/api/docs`, search for humans and bots, API keys (`sd_…`), optional OpenSubsonic, signed webhooks. Stream URLs only. Never filesystem paths.
- **Web/PWA.** Queue, ReplayGain, optional crossfade. Artists, albums, tracks, playlists, favourites.
- **Discord.** Optional OAuth sign-in (server/role registration whitelist). Optional native voice worker that plays **your** library. No Lavalink, YouTube, or Spotify.
- **Playlist matching.** Connect Spotify, YouTube, SoundCloud, or Apple Music and import playlist URLs. Titles are matched against **your** library. Provider audio is not downloaded.
- **Operations.** PostgreSQL system of record. Optional Redis, Meilisearch, Prometheus `/metrics`, Cloudflare Tunnel. Admin for users, roles, jobs, backups, transcoding, retention.

## Docker / development

```bash
cp .env.example .env
# set POSTGRES_PASSWORD, SD_MASTER_KEY
# optional Discord: SD_DISCORD_ENABLED=true, SD_DISCORD_CLIENT_ID, SD_DISCORD_CLIENT_SECRET, SD_ADMIN_DISCORD_ID
docker compose up -d --build
```

Production hosts should `docker compose pull && docker compose up -d` so they use `ghcr.io/skila1/sounddock:latest` instead of cloning.

Discord bot worker: `docker compose --profile discord up -d`  
Cloudflare Tunnel: `docker compose --profile cloudflare up -d`

```bash
# PostgreSQL 16, then:
export SD_DATABASE_URL='postgres://sounddock:sounddock@127.0.0.1:5432/sounddock?sslmode=disable'
export SD_MASTER_KEY='dev-master-key-change-me'
cd web && npm install && npm run build && cd ..
go run ./cmd/sounddock all
```

## Documentation

- [Installation](docs/install.md)
- [Architecture](docs/architecture.md)
- [API](docs/api.md)
- [Docker](docs/docker.md)
- [Environment](docs/environment.md)
- [Security](SECURITY.md)
- [Discord](docs/discord.md)
- [Wiki](https://github.com/Skila1/SoundDock/wiki)
