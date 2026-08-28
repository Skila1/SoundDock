# Docker

Published image: `ghcr.io/sounddock/sounddock:latest`

Image CI lives in `.github/workflows/docker.yml` and **only runs on GitHub** (`github.server_url == https://github.com`). A Gitea mirror is an archive: if Gitea Actions picks up the same file, the job is skipped and nothing is built or pushed.

**Operators** (no git clone):

```bash
curl -fsSL https://raw.githubusercontent.com/sounddock/sounddock/main/install.sh | sudo bash
```

or copy `docker-compose.yml` + `.env` and run `docker compose pull && docker compose up -d`.

This repo: `docker compose up -d --build` still builds a local image tagged as the published name. YouTube search and download run inside the SoundDock container (yt-dlp + ffmpeg). There is no ScapeX sidecar in Compose. `SD_SCAPEX_URL` is deprecated; leave it empty.

`discord-worker` starts with the default stack (not a Compose profile). Optional profiles:

- `redis`
- `search` (Meilisearch)

Cloudflare Tunnel is installed by the installer as a **systemd** service (`cloudflared`), not a Compose profile. Origin: `http://localhost:8080`.

Health: `/healthz`, `/readyz`. Stop grace period is 45s for FFmpeg and Discord drain.

Worker pool **Memory cap (MB, advisory)** in Admin → Workers is not a Docker/cgroup memory limit.
