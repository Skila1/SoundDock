# Environment

See `.env.example`. Important:

| Variable | Purpose |
|---|---|
| `SD_MASTER_KEY` | Encrypts storage secrets, Discord token, webhooks |
| `SD_PUBLIC_URL` | Cookies, linking URLs, stream hosts |
| `SD_TRUSTED_PROXIES` | CIDRs allowed to set `X-Forwarded-For` |
| `SD_METRICS_ENABLED` | Optional Prometheus `/metrics` |
| `SD_DISCORD_ENABLED` | `true` (default) to allow Discord OAuth; `false` for local username/password only |
| `SD_DISCORD_CLIENT_ID` | Discord OAuth application client ID |
| `SD_DISCORD_CLIENT_SECRET` | Discord OAuth client secret |
| `SD_DISCORD_BOT_TOKEN` | Optional bot token; also configurable in Admin |
| `SD_ADMIN_DISCORD_ID` | Discord user snowflake(s) granted Administrator on every launch when Discord is on |
| `SD_IMAGE` | Image to pull (`ghcr.io/sounddock/sounddock:latest`) |
