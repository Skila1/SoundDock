# Environment

See `.env.example`. The installer only writes what is needed to boot. Discord client ID, secret, bot token, and admin Discord IDs are set in **Admin → Discord**, not in `.env`.

| Variable | Purpose |
|---|---|
| `SD_MASTER_KEY` | Encrypts storage secrets, Discord token, webhooks. `{SD_DATA_DIR}/master.key` wins if that file is present (written by restore). |
| `SD_PUBLIC_URL` | Optional override. If unset, SoundDock uses the request Host (Docker port or Cloudflare Tunnel) |
| `SD_TRUSTED_PROXIES` | CIDRs allowed to set `X-Forwarded-For` / `X-Forwarded-Proto` |
| `SD_METRICS_ENABLED` | Optional Prometheus `/metrics` |
| `SD_IMAGE` | Image to pull (`ghcr.io/skila1/sounddock:latest`) |
| `SD_LIBRARY_HOST` | Host folder mounted at `/libraries` |
| `SD_SCAPEX_URL` | **Deprecated.** Leave empty. YouTube search and fetch run in-process (yt-dlp). If set, SoundDock still talks to that leftover sidecar. The sidecar is not in Compose. |
