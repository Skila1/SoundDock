#!/usr/bin/env bash
# SoundDock one-click installer. Pulls the published Docker image. No git clone required.
#
#   curl -fsSL https://raw.githubusercontent.com/sounddock/sounddock/main/install.sh | sudo bash
#   sudo bash install.sh install|status|update|logs|uninstall|doctor
#
set -euo pipefail

PREFIX="${SOUNDDOCK_PREFIX:-/opt/sounddock}"
IMAGE="${SOUNDDOCK_IMAGE:-ghcr.io/sounddock/sounddock:latest}"
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
      SOUNDDOCK_ROLE: all
      SOUNDDOCK_DATABASE_URL: postgres://\${POSTGRES_USER:-sounddock}:\${POSTGRES_PASSWORD}@postgres:5432/\${POSTGRES_DB:-sounddock}?sslmode=disable
    volumes:
      - sounddock_data:/data
      - \${SOUNDDOCK_LIBRARY_HOST:-./libraries}:/libraries:ro
    ports:
      - "\${SOUNDDOCK_PORT:-8080}:8080"
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
      SOUNDDOCK_ROLE: discord
      SOUNDDOCK_DATABASE_URL: postgres://\${POSTGRES_USER:-sounddock}:\${POSTGRES_PASSWORD}@postgres:5432/\${POSTGRES_DB:-sounddock}?sslmode=disable
      SOUNDDOCK_HTTP_ADDR: ":8081"
    volumes:
      - sounddock_data:/data
      - \${SOUNDDOCK_LIBRARY_HOST:-./libraries}:/libraries:ro
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
  echo "SoundDock uses Discord sign-in only."
  echo "Create an app at https://discord.com/developers/applications"
  echo "  OAuth2 → Redirects:  <your-public-url>/api/v1/auth/discord/callback"
  echo "  Scopes: identify"
  echo
  read -r -p "Public URL [http://127.0.0.1:8080]: " public
  public="${public:-http://127.0.0.1:8080}"
  read -r -p "Discord client ID: " dcid
  read -r -p "Discord client secret: " dcsec
  read -r -p "Admin Discord user ID (snowflake): " adminid
  read -r -p "Host library folder [${PREFIX}/libraries]: " libhost
  libhost="${libhost:-${PREFIX}/libraries}"
  mkdir -p "${libhost}"

  if [[ -z "${dcid}" || -z "${dcsec}" || -z "${adminid}" ]]; then
    echo "Discord client ID, secret, and admin user ID are required." >&2
    exit 1
  fi

  local mk pw
  mk="$(rand)"
  pw="$(rand)"
  if [[ -f "${PREFIX}/.env" ]] && grep -q SOUNDDOCK_MASTER_KEY "${PREFIX}/.env"; then
    echo "Keeping existing ${PREFIX}/.env secrets; updating Discord settings."
    grep -q SOUNDDOCK_DISCORD_CLIENT_ID "${PREFIX}/.env" || echo "SOUNDDOCK_DISCORD_CLIENT_ID=${dcid}" >> "${PREFIX}/.env"
    # rewrite discord keys
    sed -i.bak \
      -e "s|^SOUNDDOCK_PUBLIC_URL=.*|SOUNDDOCK_PUBLIC_URL=${public}|" \
      -e "s|^SOUNDDOCK_DISCORD_CLIENT_ID=.*|SOUNDDOCK_DISCORD_CLIENT_ID=${dcid}|" \
      -e "s|^SOUNDDOCK_DISCORD_CLIENT_SECRET=.*|SOUNDDOCK_DISCORD_CLIENT_SECRET=${dcsec}|" \
      -e "s|^SOUNDDOCK_ADMIN_DISCORD_ID=.*|SOUNDDOCK_ADMIN_DISCORD_ID=${adminid}|" \
      "${PREFIX}/.env" || true
  else
    cat > "${PREFIX}/.env" <<EOF
SOUNDDOCK_ROLE=all
SOUNDDOCK_HTTP_ADDR=:8080
SOUNDDOCK_PUBLIC_URL=${public}
SOUNDDOCK_INSTANCE_NAME=SoundDock
SOUNDDOCK_DATABASE_URL=postgres://sounddock:${pw}@postgres:5432/sounddock?sslmode=disable
SOUNDDOCK_MASTER_KEY=${mk}
SOUNDDOCK_DATA_DIR=/data
SOUNDDOCK_CACHE_DIR=/data/cache
SOUNDDOCK_BACKUP_DIR=/data/backups
SOUNDDOCK_MANAGED_DIR=/data/managed
SOUNDDOCK_COOKIE_SECURE=false
SOUNDDOCK_DISCORD_CLIENT_ID=${dcid}
SOUNDDOCK_DISCORD_CLIENT_SECRET=${dcsec}
SOUNDDOCK_ADMIN_DISCORD_ID=${adminid}
SOUNDDOCK_IMAGE=${IMAGE}
SOUNDDOCK_PORT=8080
POSTGRES_USER=sounddock
POSTGRES_PASSWORD=${pw}
POSTGRES_DB=sounddock
EOF
    chmod 0600 "${PREFIX}/.env"
  fi

  local profiles=()
  echo
  read -r -p "Enable native Discord bot worker now? [y/N]: " dcbot
  if [[ "${dcbot}" =~ ^[Yy] ]]; then
    read -r -p "Discord bot token (optional, can set later): " bottok
    if [[ -n "${bottok}" ]]; then
      echo "SOUNDDOCK_DISCORD_BOT_TOKEN=${bottok}" >> "${PREFIX}/.env"
    fi
    profiles+=("discord")
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
  echo "Pulling ${IMAGE}…"
  SOUNDDOCK_LIBRARY_HOST="${libhost}" SOUNDDOCK_IMAGE="${IMAGE}" ${COMPOSE} "${pflag[@]}" pull
  SOUNDDOCK_LIBRARY_HOST="${libhost}" SOUNDDOCK_IMAGE="${IMAGE}" ${COMPOSE} "${pflag[@]}" up -d
  echo
  echo "SoundDock is starting."
  echo "  URL: ${public}"
  echo "  Sign in with Discord. Admin is Discord user ${adminid}."
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
