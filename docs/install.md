# Installation

One command. A whiptail TUI (Proxmox helper style) collects options, installs Docker if needed, and writes a **Docker Compose project** (default `/opt/sounddock`). Do not `apt install docker`.

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

Use Tab / Enter. Spacebar on any checklist. Cancel aborts. The wizard asks for the install directory (Compose project path), public URL, library folder, Discord, bot, and Cloudflare Tunnel.

After install, that directory is a normal Compose project (`.env` + `docker-compose.yml`):

```bash
cd /opt/sounddock
docker compose ps
docker compose logs -f
docker compose down
docker compose pull && docker compose up -d
```

`SD_IMAGE`, `SD_LIBRARY_HOST`, and `COMPOSE_PROFILES` are stored in `.env`, so you do not pass extra flags. Optional helper: `sudo sounddock status|update|logs|doctor|uninstall`.

Image: `ghcr.io/skila1/sounddock:latest`. Unattended:

```bash
sudo env SD_UNATTENDED=1 SD_PUBLIC_URL=https://music.example.com bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

Discord OAuth redirect if you enable it in the TUI: `{PUBLIC_URL}/api/v1/auth/discord/callback`.

## Development

See the README. PostgreSQL 16 is required. FFmpeg is optional for transcoding and Discord PCM.
