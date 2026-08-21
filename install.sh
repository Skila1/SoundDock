#!/usr/bin/env bash
# SoundDock one-click installer. Pulls the published Docker image. No git clone required.
#
#   curl -fsSL https://raw.githubusercontent.com/sounddock/sounddock/main/install.sh | sudo bash
#   sudo bash install.sh install|status|update|logs|uninstall|doctor
#
set -euo pipefail

PREFIX="${SD_PREFIX:-/opt/sounddock}"
IMAGE="${SD_IMAGE:-ghcr.io/sounddock/sounddock:latest}"
COMPOSE="docker compose"
CMD="${1:-install}"

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "Run as root (sudo)." >&2
    exit 1
  fi
}

detect_os() {
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    echo "${ID}"
  else
    echo "unknown"
  fi
}

install_docker() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    return
  fi
  echo "Installing Docker Engine + Compose plugin…"
  local os
  os="$(detect_os)"
  case "$os" in
    ubuntu|debian)
      apt-get update -y
      apt-get install -y ca-certificates curl
      curl -fsSL https://get.docker.com | sh
      ;;
    fedora|rhel|centos)
      dnf install -y docker docker-compose-plugin || yum install -y docker
      systemctl enable --now docker
      ;;
    arch)
      pacman -Sy --noconfirm docker docker-compose
      systemctl enable --now docker
      ;;
    *)
      echo "Install Docker Desktop / Engine with the Compose plugin, then re-run." >&2
      exit 1
      ;;
  esac
}

rand() { openssl rand -base64 32 | tr -d '\n/=+' | head -c 48; }

write_compose() {
  cat > "${PREFIX}/docker-compose.yml" <<EOF
services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: \${POSTGRES_USER:-sounddock}
      POSTGRES_PASSWORD: \${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD}
      POSTGRES_DB: \${POSTGRES_DB:-sounddock}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U \${POSTGRES_USER:-sounddock} -d \${POSTGRES_DB:-sounddock}"]
      interval: 5s
      timeout: 5s
      retries: 20
    networks: [sounddock]

  sounddock:
    image: ${IMAGE}
    command: ["all"]
    restart: unless-stopped
    stop_grace_period: 45s
    depends_on:
      postgres:
        condition: service_healthy
    env_file: [.env]
    environment:
      SD_ROLE: all
      SD_DATABASE_URL: postgres://\${POSTGRES_USER:-sounddock}:\${POSTGRES_PASSWORD}@postgres:5432/\${POSTGRES_DB:-sounddock}?sslmode=disable
    volumes:
      - sounddock_data:/data
      - \${SD_LIBRARY_HOST:-./libraries}:/libraries:ro
    ports:
      - "\${SD_PORT:-8080}:8080"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/healthz"]
      interval: 10s
      timeout: 5s
      retries: 12
      start_period: 25s
    networks: [sounddock]

  discord-worker:
    image: ${IMAGE}
    command: ["discord"]
    restart: unless-stopped
    profiles: ["discord"]
    depends_on:
      postgres:
        condition: service_healthy
      sounddock:
        condition: service_healthy
    env_file: [.env]
    environment:
      SD_ROLE: discord
      SD_DATABASE_URL: postgres://\${POSTGRES_USER:-sounddock}:\${POSTGRES_PASSWORD}@postgres:5432/\${POSTGRES_DB:-sounddock}?sslmode=disable
      SD_HTTP_ADDR: ":8081"
    volumes:
      - sounddock_data:/data
      - \${SD_LIBRARY_HOST:-./libraries}:/libraries:ro
    networks: [sounddock]

  cloudflared:
    image: cloudflare/cloudflared:latest
    restart: unless-stopped
    profiles: ["cloudflare"]
    command: tunnel --no-autoupdate run --token \${CLOUDFLARE_TUNNEL_TOKEN:-}
    depends_on:
      sounddock:
        condition: service_healthy
    networks: [sounddock]

volumes:
  postgres_data:
  sounddock_data:

networks:
  sounddock:
    driver: bridge
EOF
}

cmd_install() {
  need_root
  install_docker
  mkdir -p "${PREFIX}/data/cache" "${PREFIX}/data/backups" "${PREFIX}/data/managed" "${PREFIX}/libraries"
  write_compose

  echo
  read -r -p "Public URL [http://127.0.0.1:8080]: " public
  public="${public:-http://127.0.0.1:8080}"
  read -r -p "Host library folder [${PREFIX}/libraries]: " libhost
  libhost="${libhost:-${PREFIX}/libraries}"
  mkdir -p "${libhost}"

  echo
  echo "Discord sign-in is optional. If you skip it, you create a local administrator in the web UI."
  read -r -p "Enable Discord sign-in (including Discord admin)? [Y/n]: " usedc
  local dcid="" dcsec="" adminid="" dcenabled="true"
  if [[ "${usedc}" =~ ^[Nn] ]]; then
    dcenabled="false"
  else
    echo "Create an app at https://discord.com/developers/applications"
    echo "  OAuth2 Redirects:  ${public}/api/v1/auth/discord/callback"
    echo "  Scopes: identify (guilds / guilds.members.read if you whitelist a server or role later)"
    read -r -p "Discord client ID: " dcid
    read -r -p "Discord client secret: " dcsec
    read -r -p "Admin Discord user ID (snowflake, blank for none): " adminid
    if [[ -z "${dcid}" || -z "${dcsec}" ]]; then
      echo "Discord client ID and secret are required when Discord sign-in is enabled." >&2
      exit 1
    fi
  fi

  local mk pw
  mk="$(rand)"
  pw="$(rand)"
  if [[ -f "${PREFIX}/.env" ]] && grep -q SD_MASTER_KEY "${PREFIX}/.env"; then
    echo "Keeping existing ${PREFIX}/.env secrets; updating settings."
    grep -q SD_DISCORD_ENABLED "${PREFIX}/.env" || echo "SD_DISCORD_ENABLED=${dcenabled}" >> "${PREFIX}/.env"
    grep -q SD_DISCORD_CLIENT_ID "${PREFIX}/.env" || echo "SD_DISCORD_CLIENT_ID=${dcid}" >> "${PREFIX}/.env"
    sed -i.bak \
      -e "s|^SD_PUBLIC_URL=.*|SD_PUBLIC_URL=${public}|" \
      -e "s|^SD_DISCORD_ENABLED=.*|SD_DISCORD_ENABLED=${dcenabled}|" \
      -e "s|^SD_DISCORD_CLIENT_ID=.*|SD_DISCORD_CLIENT_ID=${dcid}|" \
      -e "s|^SD_DISCORD_CLIENT_SECRET=.*|SD_DISCORD_CLIENT_SECRET=${dcsec}|" \
      -e "s|^SD_ADMIN_DISCORD_ID=.*|SD_ADMIN_DISCORD_ID=${adminid}|" \
      "${PREFIX}/.env" || true
  else
    cat > "${PREFIX}/.env" <<EOF
SD_ROLE=all
SD_HTTP_ADDR=:8080
SD_PUBLIC_URL=${public}
SD_INSTANCE_NAME=SoundDock
SD_DATABASE_URL=postgres://sounddock:${pw}@postgres:5432/sounddock?sslmode=disable
SD_MASTER_KEY=${mk}
SD_DATA_DIR=/data
SD_CACHE_DIR=/data/cache
SD_BACKUP_DIR=/data/backups
SD_MANAGED_DIR=/data/managed
SD_COOKIE_SECURE=false
SD_DISCORD_ENABLED=${dcenabled}
SD_DISCORD_CLIENT_ID=${dcid}
SD_DISCORD_CLIENT_SECRET=${dcsec}
SD_ADMIN_DISCORD_ID=${adminid}
SD_IMAGE=${IMAGE}
SD_PORT=8080
POSTGRES_USER=sounddock
POSTGRES_PASSWORD=${pw}
POSTGRES_DB=sounddock
EOF
    chmod 0600 "${PREFIX}/.env"
  fi

  local profiles=()
  echo
  if [[ "${dcenabled}" == "true" ]]; then
    read -r -p "Enable native Discord bot worker now? [y/N]: " dcbot
    if [[ "${dcbot}" =~ ^[Yy] ]]; then
      read -r -p "Discord bot token (optional, can set later): " bottok
      if [[ -n "${bottok}" ]]; then
        echo "SD_DISCORD_BOT_TOKEN=${bottok}" >> "${PREFIX}/.env"
      fi
      profiles+=("discord")
    fi
  fi
  read -r -p "Cloudflare Tunnel token (blank to skip): " cftok
  if [[ -n "${cftok}" ]]; then
    echo "CLOUDFLARE_TUNNEL_TOKEN=${cftok}" >> "${PREFIX}/.env"
    profiles+=("cloudflare")
  fi

  local pflag=()
  for p in "${profiles[@]+"${profiles[@]}"}"; do
    pflag+=(--profile "$p")
  done

  cd "${PREFIX}"
  echo "Pulling ${IMAGE}..."
  SD_LIBRARY_HOST="${libhost}" SD_IMAGE="${IMAGE}" ${COMPOSE} "${pflag[@]}" pull
  SD_LIBRARY_HOST="${libhost}" SD_IMAGE="${IMAGE}" ${COMPOSE} "${pflag[@]}" up -d
  echo
  echo "SoundDock is starting."
  echo "  URL: ${public}"
  if [[ "${dcenabled}" == "true" ]]; then
    echo "  Sign in with Discord. Admin Discord ID: ${adminid:-none}"
  else
    echo "  Discord is off. Open the URL and create the first administrator."
  fi
  echo "  Data: ${PREFIX}/data"
  echo "  Control: sudo $0 status|update|logs|uninstall|doctor"
}

cmd_status() { cd "${PREFIX}" && ${COMPOSE} ps; }
cmd_logs() { cd "${PREFIX}" && ${COMPOSE} logs -f --tail=200; }
cmd_update() {
  cd "${PREFIX}"
  ${COMPOSE} pull
  ${COMPOSE} up -d
}
cmd_uninstall() {
  need_root
  cd "${PREFIX}"
  ${COMPOSE} down
  if [[ "${1:-}" == "--purge" ]]; then
    ${COMPOSE} down -v
    rm -rf "${PREFIX}"
  else
    echo "Data kept in ${PREFIX}/data (pass --purge to delete)."
  fi
}
cmd_doctor() {
  command -v docker
  docker compose version
  curl -fsS http://127.0.0.1:8080/healthz || true
}

case "$CMD" in
  install) cmd_install ;;
  status) cmd_status ;;
  update) cmd_update ;;
  uninstall) cmd_uninstall "${2:-}" ;;
  logs) cmd_logs ;;
  doctor) cmd_doctor ;;
  *) echo "Usage: $0 install|status|update|logs|uninstall|doctor" ;;
esac
