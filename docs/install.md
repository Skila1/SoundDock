# Installation

Sign-in is **Discord only**. There is no first-run admin password. The administrator is `SOUNDDOCK_ADMIN_DISCORD_ID` in `.env`, applied on every container start.

## One-click (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/sounddock/sounddock/main/install.sh | sudo bash
```

Installs Docker if needed, writes `/opt/sounddock`, **pulls** `ghcr.io/sounddock/sounddock:latest`, and starts Compose. You will be asked for:

- Public URL
- Discord OAuth client ID and secret (scope `identify`, redirect `{PUBLIC_URL}/api/v1/auth/discord/callback`)
- Your Discord user ID (snowflake). This account is administrator.

```bash
sudo bash install.sh status|update|logs|uninstall|doctor
```

Uninstall keeps `/opt/sounddock/data` unless `--purge`.

## Development

See the README. PostgreSQL 16 is required. FFmpeg is optional for transcoding and Discord PCM.
