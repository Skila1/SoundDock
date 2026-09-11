# Environment

See `.env.example`. The installer only writes what is needed to boot. Discord client ID, secret, bot token, and admin Discord IDs are set in **Admin → Discord**, not in `.env`.

| Variable | Purpose |
|---|---|
| `SD_MASTER_KEY` | Encrypts storage secrets, Discord token, webhooks. Empty or the documented placeholder is rejected at boot. `{SD_DATA_DIR}/master.key` wins if that file is present (written by restore). |
| `SD_PUBLIC_URL` | Public origin used for CORS, cookies, and OAuth redirects. In production set this to the Cloudflare Tunnel hostname. If unset, SoundDock uses the request Host (Docker port or forwarded Host from a trusted proxy). |
| `SD_TRUSTED_PROXIES` | CIDRs allowed to set `X-Forwarded-For` / `X-Forwarded-Proto` / `X-Forwarded-Host` |
| `SD_METRICS_ENABLED` | Optional Prometheus `/metrics` |
| `SD_IMAGE` | Image to pull (`ghcr.io/skila1/sounddock:latest`) |
| `SD_LIBRARY_HOST` | Host folder mounted at `/libraries` |
| `SD_SCAPEX_URL` | **Deprecated.** Leave empty. YouTube search and fetch run in-process (yt-dlp). If set, SoundDock still talks to that leftover sidecar. The sidecar is not in Compose. |
| `SD_OPENSUBSONIC` | OpenSubsonic adapter is a stub. Leave `false`. |

CORS allows `http://localhost:5173`, `http://127.0.0.1:5173`, and the origin of `SD_PUBLIC_URL`. It does not allow arbitrary browser origins.

Worker pool **Memory cap (MB, advisory)** in Admin → Workers is a stored hint. It is not a Docker or cgroup memory limit.
