# Installation

One command. A whiptail TUI (Proxmox helper style) collects options, installs Docker if needed, writes `/opt/sounddock`, pulls the image, and starts SoundDock. Do not `apt install docker`.

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

Use Tab / Enter. Spacebar on any checklist. Cancel aborts.

```bash
sudo sounddock status
sudo sounddock update
sudo sounddock logs
sudo sounddock doctor
sudo sounddock uninstall          # keeps /opt/sounddock/data
sudo sounddock uninstall --purge
```

Image: `ghcr.io/skila1/sounddock:latest`. Unattended:

```bash
sudo env SD_UNATTENDED=1 SD_PUBLIC_URL=https://music.example.com bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
```

Discord OAuth redirect if you enable it in the TUI: `{PUBLIC_URL}/api/v1/auth/discord/callback`.

## Development

See the README. PostgreSQL 16 is required. FFmpeg is optional for transcoding and Discord PCM.
