# Installation

One command. A whiptail TUI writes a Docker Compose project (default `/opt/sounddock`). Docker publishes port 8080. Optional Cloudflare Tunnel is the public URL. Do not `apt install docker`.

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

The wizard asks for install directory, library folder, and optional Cloudflare Tunnel. It writes `docker-compose.yml` and `.env` into that directory (default `/opt/sounddock`). Paths like `~/sounddock` are expanded to an absolute path. Cloudflared, if enabled, is a systemd service. It does not ask for an IP, a public URL, or Discord credentials.

Open `http://<host>:8080` (or your tunnel) and create the first local administrator. Configure Discord later under **Admin → Discord**. The first Discord user to sign in (when there is not already an administrator) is marked administrator.

```bash
cd /opt/sounddock
docker compose ps
docker compose logs -f
docker compose down
docker compose pull && docker compose up -d
```

Unattended:

```bash
sudo env SD_UNATTENDED=1 bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

## Development

See the README. PostgreSQL 16 is required. FFmpeg is optional for transcoding and Discord PCM.
