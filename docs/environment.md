# Environment

See `.env.example`. The installer only writes what is needed to boot. Discord client ID, secret, bot token, and admin Discord IDs are set in **Admin → Discord**, not in `.env`.

| Variable | Purpose |
|---|---|
| `SD_MASTER_KEY` | Encrypts storage secrets, Discord token, webhooks |
| `SD_PUBLIC_URL` | Optional override. If unset, SoundDock uses the request Host (Docker port or Cloudflare Tunnel) |
| `SD_TRUSTED_PROXIES` | CIDRs allowed to set `X-Forwarded-For` / `X-Forwarded-Proto` |
| `SD_METRICS_ENABLED` | Optional Prometheus `/metrics` |
| `SD_IMAGE` | Image to pull (`ghcr.io/skila1/sounddock:latest`) |
| `SD_LIBRARY_HOST` | Host folder mounted at `/libraries` |
| `SD_SCAPEX_URL` | ScapeX sidecar on the compose network (`http://scapex:7788`). Empty disables YouTube search/fetch |
