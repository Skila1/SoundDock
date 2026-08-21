# Installation

The installer asks whether to use Discord. If you skip Discord, open the web UI and create the first administrator with a username and password.

If Discord is on, `SD_ADMIN_DISCORD_ID` in `.env` is granted Administrator on every start. Leave it empty if you do not want that.

## One-click (recommended)

```bash
export SD_IMAGE=ghcr.io/skila1/sounddock:latest
curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh | sudo bash
```

Installs Docker if needed, writes `/opt/sounddock`, pulls the published image, and starts Compose. You will be asked for:

- Public URL
- Whether to enable Discord sign-in
- If yes: Discord OAuth client ID and secret (redirect `{PUBLIC_URL}/api/v1/auth/discord/callback`) and optional admin Discord user ID

```bash
sudo bash install.sh status|update|logs|uninstall|doctor
```

Uninstall keeps `/opt/sounddock/data` unless `--purge`.

## Development

See the README. PostgreSQL 16 is required. FFmpeg is optional for transcoding and Discord PCM.
