# SoundDock

**Your music. Your server. Your way.**

SoundDock is an open-source, self-hosted music platform for organising, streaming and integrating your own music library.

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

A whiptail installer (same idea as Proxmox helper scripts) writes a Docker Compose project in `./sounddock` under your current directory (`~/sounddock` if you run it from home). If you are already in a folder named `sounddock`, it installs there. It installs Docker if missing and starts the stack. Docker publishes port 8080. Cloudflare Tunnel is optional. Discord is configured in the web Admin after first launch. Then you manage it like any Compose stack:

```bash
cd ~/sounddock
docker compose ps
docker compose logs -f
docker compose down
docker compose pull && docker compose up -d
```

Optional helper: `sudo sounddock status|update|logs|doctor|uninstall` (same directory).

Unattended (no TUI):

```bash
sudo env SD_UNATTENDED=1 bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

You bring the files. SoundDock does not provide catalogues, rip YouTube or Spotify, or bypass DRM.

License: **GNU Affero General Public License v3.0 or later**.

## Music infrastructure

SoundDock is built to be the system your library, players, and bots talk to. The web player and native Discord worker are first-party clients of the same API.

- **Storage.** Libraries on local disk, NAS/NFS/SMB, Docker volumes, and S3-compatible object storage (Cloudflare R2, AWS S3, MinIO, B2). Scan in place, resumable uploads, Remote Import of direct HTTP(S) media URLs, migrate into managed storage.
- **API.** REST `/api/v1`, OpenAPI at `/api/docs`, search for humans and bots, API keys (`sd_…`), optional OpenSubsonic, signed webhooks. Stream URLs only. Never filesystem paths.
- **Web/PWA.** Queue, ReplayGain, optional crossfade. Artists, albums, tracks, playlists, favourites.
- **Discord.** Optional OAuth sign-in (server/role registration whitelist). Optional native voice worker that plays **your** library. No Lavalink, YouTube, or Spotify. `/link` in Discord completes in the web UI.
- **Playlist matching.** Connect Spotify, YouTube, SoundCloud, or Apple Music and import playlist URLs. Titles are matched against **your** library. Provider audio is not downloaded.
- **Operations.** PostgreSQL system of record. Optional Redis, Meilisearch, Prometheus `/metrics`, Cloudflare Tunnel. Admin for users, roles, jobs, backups, transcoding, retention.

## Docker / development

```bash
cp .env.example .env
# set POSTGRES_PASSWORD, SD_MASTER_KEY
# Discord is configured in Admin after first launch
docker compose up -d --build
```

Production hosts should `docker compose pull && docker compose up -d` so they use `ghcr.io/skila1/sounddock:latest` instead of cloning.

The Discord voice worker (`discord-worker`) starts with the default Compose stack.

Cloudflare Tunnel is a **systemd** service (`cloudflared`), not Compose. Point the tunnel at `http://localhost:8080`.

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
