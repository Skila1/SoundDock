# Environment

See `.env.example`. Important:

| Variable | Purpose |
|---|---|
| `SOUNDDOCK_MASTER_KEY` | Encrypts storage secrets, Discord token, webhooks |
| `SOUNDDOCK_PUBLIC_URL` | Cookies, linking URLs, stream hosts |
| `SOUNDDOCK_TRUSTED_PROXIES` | CIDRs allowed to set `X-Forwarded-For` |
| `SOUNDDOCK_METRICS_ENABLED` | Optional Prometheus `/metrics` |
| `SOUNDDOCK_DISCORD_CLIENT_ID` | Discord OAuth application client ID (required for sign-in) |
| `SOUNDDOCK_DISCORD_CLIENT_SECRET` | Discord OAuth client secret |
| `SOUNDDOCK_DISCORD_BOT_TOKEN` | Optional bot token; also configurable in Admin |
| `SOUNDDOCK_ADMIN_DISCORD_ID` | Discord user snowflake(s) granted Administrator on every launch |
| `SOUNDDOCK_IMAGE` | Image to pull (`ghcr.io/sounddock/sounddock:latest`) |
