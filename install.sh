#!/usr/bin/env bash
# SoundDock installer. Whiptail TUI (Proxmox helper style). Do not apt install docker.
#
#   sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
#
# After install: sudo sounddock status|update|logs|uninstall|doctor
# Unattended:    sudo env SD_UNATTENDED=1 bash -c "$(curl -fsSL ...)"
#
set -euo pipefail

PREFIX="${SD_PREFIX:-/opt/sounddock}"
IMAGE="${SD_IMAGE:-ghcr.io/skila1/sounddock:latest}"
SCRIPT_SRC="${SD_INSTALL_URL:-https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh}"
COMPOSE="docker compose"
CMD="${1:-install}"
BACKTITLE="SoundDock Installer"
export TERM="${TERM:-xterm}"

YW=$'\033[33m'
GN=$'\033[1;92m'
BL=$'\033[36m'
RD=$'\033[1;31m'
CL=$'\033[m'
BOLD=$'\033[1m'

msg_info() { echo -e " ${YW}[*]${CL} $1"; }
msg_ok() { echo -e " ${GN}[ok]${CL} $1"; }
msg_err() { echo -e " ${RD}[err]${CL} $1" >&2; }

header_info() {
  if [[ -e /dev/tty ]]; then
    clear >/dev/tty 2>/dev/null || true
    cat >/dev/tty <<'EOF'

  ███████╗ ██████╗ ██╗   ██╗███╗   ██╗██████╗ ██████╗  ██████╗  ██████╗██╗  ██╗
  ██╔════╝██╔═══██╗██║   ██║████╗  ██║██╔══██╗██╔══██╗██╔═══██╗██╔════╝██║ ██╔╝
  ███████╗██║   ██║██║   ██║██╔██╗ ██║██║  ██║██║  ██║██║   ██║██║     █████╔╝
  ╚════██║██║   ██║██║   ██║██║╚██╗██║██║  ██║██║  ██║██║   ██║██║     ██╔═██╗
  ███████║╚██████╔╝╚██████╔╝██║ ╚████║██████╔╝██████╔╝╚██████╔╝╚██████╗██║  ██╗
  ╚══════╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═══╝╚═════╝ ╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝

  Your music. Your server. Your way.

EOF
  fi
}

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

ensure_whiptail() {
  if command -v whiptail >/dev/null 2>&1; then
    return
  fi
  msg_info "Installing whiptail"
  case "$(detect_os)" in
    ubuntu|debian)
      apt-get update -y
      apt-get install -y whiptail ca-certificates curl
      ;;
    fedora|rhel|centos)
      dnf install -y newt ca-certificates curl || yum install -y newt ca-certificates curl
      ;;
    arch)
      pacman -Sy --noconfirm libnewt curl
      ;;
    *)
      msg_err "Install whiptail (newt), then re-run."
      exit 1
      ;;
  esac
  msg_ok "whiptail ready"
}

ui_ok() {
  [[ -e /dev/tty && "${SD_UNATTENDED:-0}" != "1" ]] && command -v whiptail >/dev/null 2>&1
}

# Dialogs talk to the real terminal so curl|bash still works.
ui_yesno() {
  whiptail --backtitle "${BACKTITLE}" --title "$1" --yesno "$2" "${3:-12}" 72 </dev/tty
}

ui_msg() {
  whiptail --backtitle "${BACKTITLE}" --title "$1" --msgbox "$2" "${3:-12}" 72 </dev/tty
}

ui_input() {
  local out
  out=$(whiptail --backtitle "${BACKTITLE}" --title "$1" --inputbox "$2" 12 72 "$3" 3>&1 1>&2 2>&3 </dev/tty) || return 1
  printf '%s' "${out}"
}

ui_pass() {
  local out
  out=$(whiptail --backtitle "${BACKTITLE}" --title "$1" --passwordbox "$2" 12 72 3>&1 1>&2 2>&3 </dev/tty) || return 1
  printf '%s' "${out}"
}

install_docker() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    systemctl enable --now docker 2>/dev/null || true
    msg_ok "Docker already installed"
    return
  fi
  msg_info "Installing Docker Engine + Compose plugin"
  local os
  os="$(detect_os)"
  case "$os" in
    ubuntu|debian)
      apt-get update -y
      apt-get install -y ca-certificates curl
      curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
      sh /tmp/get-docker.sh
      rm -f /tmp/get-docker.sh
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
      msg_err "Install Docker Engine with the Compose plugin, then re-run."
      exit 1
      ;;
  esac
  systemctl enable --now docker 2>/dev/null || true
  if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
    msg_err "Docker Engine or the Compose plugin is missing after install."
    exit 1
  fi
  msg_ok "Docker installed"
}

rand() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 36 | tr -d '\n/=+' | head -c 48
  else
    tr -dc 'A-Za-z0-9' </dev/urandom | head -c 48
  fi
}

default_public_url() {
  if [[ -n "${SD_PUBLIC_URL:-}" ]]; then
    echo "${SD_PUBLIC_URL}"
    return
  fi
  local ip
  ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  if [[ -n "${ip}" ]]; then
    echo "http://${ip}:8080"
  else
    echo "http://127.0.0.1:8080"
  fi
}

# Filled by collect_config
CFG_PUBLIC=""
CFG_LIBHOST=""
CFG_DC="false"
CFG_DCID=""
CFG_DCSEC=""
CFG_ADMIN=""
CFG_BOTTOK=""
CFG_CFTOK=""

collect_config() {
  CFG_PUBLIC="$(default_public_url)"
  CFG_LIBHOST="${SD_LIBRARY_HOST:-${PREFIX}/libraries}"
  CFG_DCID="${SD_DISCORD_CLIENT_ID:-}"
  CFG_DCSEC="${SD_DISCORD_CLIENT_SECRET:-}"
  CFG_ADMIN="${SD_ADMIN_DISCORD_ID:-}"
  CFG_BOTTOK="${SD_DISCORD_BOT_TOKEN:-}"
  CFG_CFTOK="${CLOUDFLARE_TUNNEL_TOKEN:-}"
  if [[ "${SD_DISCORD_ENABLED:-}" == "true" || -n "${CFG_DCID}" ]]; then
    CFG_DC="true"
  else
    CFG_DC="false"
  fi

  if ! ui_ok; then
    msg_info "No TUI (set SD_UNATTENDED=1 or no /dev/tty). Using defaults / env."
    return
  fi

  ui_msg "SoundDock" "Use Tab and Enter to move. Use Spacebar on checklists.\n\nThis writes a Docker Compose project. After install you run docker compose from that directory (down, up, logs, pull)." 12 || true

  PREFIX="$(ui_input "Install directory" "Docker Compose project directory." "${PREFIX}")" || exit 0
  PREFIX="${PREFIX:-/opt/sounddock}"
  PREFIX="${PREFIX%/}"
  CFG_LIBHOST="${SD_LIBRARY_HOST:-${PREFIX}/libraries}"

  if ! ui_yesno "Install SoundDock" "Create a Compose project at ${PREFIX}?\n\nImage: ${IMAGE}\nThen: cd ${PREFIX} && docker compose ps"; then
    echo "Cancelled."
    exit 0
  fi

  CFG_PUBLIC="$(ui_input "Public URL" "URL you will open in a browser (no trailing slash)." "${CFG_PUBLIC}")" || exit 0
  CFG_PUBLIC="${CFG_PUBLIC:-$(default_public_url)}"
  CFG_LIBHOST="$(ui_input "Library folder" "Host folder mounted read-only at /libraries inside the container." "${CFG_LIBHOST}")" || exit 0
  CFG_LIBHOST="${CFG_LIBHOST:-${PREFIX}/libraries}"

  if ui_yesno "Discord sign-in" "Enable Discord sign-in (including Discord admin)?\n\nSelect No to create a local administrator in the browser." 13; then
    CFG_DC="true"
    ui_msg "Discord app" "Create an application at:\nhttps://discord.com/developers/applications\n\nOAuth2 Redirects:\n${CFG_PUBLIC}/api/v1/auth/discord/callback\n\nScopes: identify" 16 || true
    CFG_DCID="$(ui_input "Discord client ID" "Application client ID." "${CFG_DCID}")" || exit 0
    CFG_DCSEC="$(ui_pass "Discord client secret" "Application client secret.")" || exit 0
    CFG_ADMIN="$(ui_input "Admin Discord ID" "Your Discord user snowflake (optional, blank for none). Comma-separated for several." "${CFG_ADMIN}")" || exit 0
    if [[ -z "${CFG_DCID}" || -z "${CFG_DCSEC}" ]]; then
      ui_msg "Discord" "Client ID and secret are required when Discord sign-in is enabled." 10 || true
      exit 1
    fi
    if ui_yesno "Discord bot" "Enable the native Discord voice worker now?\n\nIt plays your SoundDock library only. No YouTube or Spotify." 13; then
      CFG_BOTTOK="$(ui_pass "Bot token" "Bot token (leave empty to set later in Admin).")" || exit 0
    fi
  else
    CFG_DC="false"
    CFG_DCID=""
    CFG_DCSEC=""
    CFG_ADMIN=""
    CFG_BOTTOK=""
  fi

  if ui_yesno "Cloudflare Tunnel" "Add a Cloudflare Tunnel token now?\n\nSelect No to skip (you can publish later)." 12; then
    CFG_CFTOK="$(ui_pass "Tunnel token" "Cloudflare Tunnel token.")" || exit 0
  fi

  local summary
  summary="Compose project: ${PREFIX}\nURL: ${CFG_PUBLIC}\nLibraries: ${CFG_LIBHOST}\nDiscord sign-in: ${CFG_DC}\nDiscord bot: $([[ -n "${CFG_BOTTOK}" ]] && echo yes || echo no)\nCloudflare Tunnel: $([[ -n "${CFG_CFTOK}" ]] && echo yes || echo no)\n\nThen: cd ${PREFIX} && docker compose ..."
  if ! ui_yesno "Ready" "${summary}\n\nStart install?" 16; then
    echo "Cancelled."
    exit 0
  fi
}

write_cli() {
  cat > /usr/local/bin/sounddock <<EOF
#!/usr/bin/env bash
set -euo pipefail
PREFIX="${PREFIX}"
COMPOSE="docker compose"
need_root() {
  if [[ "\${EUID}" -ne 0 ]]; then
    echo "Run as root (sudo sounddock ...)." >&2
    exit 1
  fi
}
need_install() {
  if [[ ! -f "\${PREFIX}/docker-compose.yml" ]]; then
    echo "SoundDock is not installed. Run:" >&2
    echo '  sudo bash -c "$(curl -fsSL ${SCRIPT_SRC})"' >&2
    exit 1
  fi
}
cd_prefix() { need_install; cd "\${PREFIX}"; }
cmd="\${1:-}"
case "\$cmd" in
  status) cd_prefix; \${COMPOSE} ps ;;
  logs) cd_prefix; \${COMPOSE} logs -f --tail=200 ;;
  update)
    cd_prefix
    \${COMPOSE} pull
    \${COMPOSE} up -d
    ;;
  uninstall)
    need_root
    cd_prefix
    if [[ "\${2:-}" == "--purge" ]]; then
      \${COMPOSE} down -v
      rm -rf "\${PREFIX}"
      rm -f /usr/local/bin/sounddock
    else
      \${COMPOSE} down
      echo "Data kept in \${PREFIX}/data (pass --purge to delete)."
    fi
    ;;
  doctor)
    command -v docker
    docker compose version
    curl -fsS http://127.0.0.1:8080/healthz || true
    echo
    if [[ -f "\${PREFIX}/docker-compose.yml" ]]; then
      cd "\${PREFIX}" && \${COMPOSE} ps
    fi
    ;;
  install)
    echo "Re-run the installer:"
    echo '  sudo bash -c "$(curl -fsSL ${SCRIPT_SRC})"'
    ;;
  *)
    echo "Usage: sudo sounddock status|update|logs|uninstall|doctor"
    echo "Or:    cd ${PREFIX} && docker compose ps|logs|down|up -d"
    exit 1
    ;;
esac
EOF
  chmod 0755 /usr/local/bin/sounddock
}

write_compose() {
  cat > "${PREFIX}/docker-compose.yml" <<EOF
name: sounddock

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
    image: \${SD_IMAGE:-ghcr.io/skila1/sounddock:latest}
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
    image: \${SD_IMAGE:-ghcr.io/skila1/sounddock:latest}
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

save_installer() {
  local src="${BASH_SOURCE[0]:-}"
  if [[ -n "${src}" && -f "${src}" && "${src}" != *bash ]]; then
    cp "${src}" "${PREFIX}/install.sh"
  else
    curl -fsSL "${SCRIPT_SRC}" -o "${PREFIX}/install.sh" || true
  fi
  if [[ -f "${PREFIX}/install.sh" ]]; then
    chmod 0755 "${PREFIX}/install.sh"
  fi
}

cmd_install() {
  need_root
  header_info
  ensure_whiptail
  collect_config

  install_docker
  mkdir -p "${PREFIX}/data/cache" "${PREFIX}/data/backups" "${PREFIX}/data/managed" "${PREFIX}/libraries"
  mkdir -p "${CFG_LIBHOST}"
  write_compose
  write_cli
  save_installer

  local mk pw
  mk="$(rand)"
  pw="$(rand)"
  if [[ -f "${PREFIX}/.env" ]] && grep -q SD_MASTER_KEY "${PREFIX}/.env"; then
    msg_info "Keeping existing ${PREFIX}/.env secrets"
    grep -q '^SD_IMAGE=' "${PREFIX}/.env" || echo "SD_IMAGE=${IMAGE}" >> "${PREFIX}/.env"
    grep -q '^SD_DISCORD_ENABLED=' "${PREFIX}/.env" || echo "SD_DISCORD_ENABLED=${CFG_DC}" >> "${PREFIX}/.env"
    grep -q '^SD_LIBRARY_HOST=' "${PREFIX}/.env" || echo "SD_LIBRARY_HOST=${CFG_LIBHOST}" >> "${PREFIX}/.env"
    sed -i.bak \
      -e "s|^SD_PUBLIC_URL=.*|SD_PUBLIC_URL=${CFG_PUBLIC}|" \
      -e "s|^SD_IMAGE=.*|SD_IMAGE=${IMAGE}|" \
      -e "s|^SD_LIBRARY_HOST=.*|SD_LIBRARY_HOST=${CFG_LIBHOST}|" \
      -e "s|^SD_DISCORD_ENABLED=.*|SD_DISCORD_ENABLED=${CFG_DC}|" \
      -e "s|^SD_DISCORD_CLIENT_ID=.*|SD_DISCORD_CLIENT_ID=${CFG_DCID}|" \
      -e "s|^SD_DISCORD_CLIENT_SECRET=.*|SD_DISCORD_CLIENT_SECRET=${CFG_DCSEC}|" \
      -e "s|^SD_ADMIN_DISCORD_ID=.*|SD_ADMIN_DISCORD_ID=${CFG_ADMIN}|" \
      "${PREFIX}/.env" || true
  else
    cat > "${PREFIX}/.env" <<EOF
SD_ROLE=all
SD_HTTP_ADDR=:8080
SD_PUBLIC_URL=${CFG_PUBLIC}
SD_INSTANCE_NAME=SoundDock
SD_DATABASE_URL=postgres://sounddock:${pw}@postgres:5432/sounddock?sslmode=disable
SD_MASTER_KEY=${mk}
SD_DATA_DIR=/data
SD_CACHE_DIR=/data/cache
SD_BACKUP_DIR=/data/backups
SD_MANAGED_DIR=/data/managed
SD_COOKIE_SECURE=false
SD_DISCORD_ENABLED=${CFG_DC}
SD_DISCORD_CLIENT_ID=${CFG_DCID}
SD_DISCORD_CLIENT_SECRET=${CFG_DCSEC}
SD_ADMIN_DISCORD_ID=${CFG_ADMIN}
SD_IMAGE=${IMAGE}
SD_LIBRARY_HOST=${CFG_LIBHOST}
SD_PORT=8080
POSTGRES_USER=sounddock
POSTGRES_PASSWORD=${pw}
POSTGRES_DB=sounddock
EOF
    chmod 0600 "${PREFIX}/.env"
  fi

  if [[ -n "${CFG_BOTTOK}" ]]; then
    grep -q '^SD_DISCORD_BOT_TOKEN=' "${PREFIX}/.env" && \
      sed -i.bak -e "s|^SD_DISCORD_BOT_TOKEN=.*|SD_DISCORD_BOT_TOKEN=${CFG_BOTTOK}|" "${PREFIX}/.env" || \
      echo "SD_DISCORD_BOT_TOKEN=${CFG_BOTTOK}" >> "${PREFIX}/.env"
  fi
  if [[ -n "${CFG_CFTOK}" ]]; then
    grep -q '^CLOUDFLARE_TUNNEL_TOKEN=' "${PREFIX}/.env" && \
      sed -i.bak -e "s|^CLOUDFLARE_TUNNEL_TOKEN=.*|CLOUDFLARE_TUNNEL_TOKEN=${CFG_CFTOK}|" "${PREFIX}/.env" || \
      echo "CLOUDFLARE_TUNNEL_TOKEN=${CFG_CFTOK}" >> "${PREFIX}/.env"
  fi

  local profiles=()
  if [[ -n "${CFG_BOTTOK}" ]] || grep -q '^SD_DISCORD_BOT_TOKEN=.\+' "${PREFIX}/.env" 2>/dev/null; then
    profiles+=(discord)
  fi
  if [[ -n "${CFG_CFTOK}" ]] || grep -q '^CLOUDFLARE_TUNNEL_TOKEN=.\+' "${PREFIX}/.env" 2>/dev/null; then
    profiles+=(cloudflare)
  fi
  local profile_csv=""
  if [[ ${#profiles[@]} -gt 0 ]]; then
    profile_csv="$(IFS=,; echo "${profiles[*]}")"
    if grep -q '^COMPOSE_PROFILES=' "${PREFIX}/.env"; then
      sed -i.bak -e "s|^COMPOSE_PROFILES=.*|COMPOSE_PROFILES=${profile_csv}|" "${PREFIX}/.env"
    else
      echo "COMPOSE_PROFILES=${profile_csv}" >> "${PREFIX}/.env"
    fi
  fi

  cd "${PREFIX}"
  msg_info "Pulling ${IMAGE} in ${PREFIX}"
  ${COMPOSE} pull
  ${COMPOSE} up -d
  msg_ok "SoundDock is starting"

  local done
  done="Compose project: ${PREFIX}\nURL: ${CFG_PUBLIC}\n\ncd ${PREFIX}\ndocker compose ps\ndocker compose logs -f\ndocker compose down\ndocker compose up -d\n"
  if [[ "${CFG_DC}" == "true" ]]; then
    done+="\nDiscord sign-in is on."
  else
    done+="\nDiscord is off. Create the first administrator in the browser."
  fi
  if ui_ok; then
    ui_msg "Installed" "${done}" 18 || true
  fi
  echo
  echo -e " ${BOLD}${BL}SoundDock is a Docker Compose project in ${PREFIX}${CL}"
  echo "  URL:  ${CFG_PUBLIC}"
  echo
  echo "  cd ${PREFIX}"
  echo "  docker compose ps"
  echo "  docker compose logs -f"
  echo "  docker compose down"
  echo "  docker compose pull && docker compose up -d"
  echo
  echo "  Optional helper: sudo sounddock status|update|logs|doctor|uninstall"
}

cmd_status() { [[ -f "${PREFIX}/docker-compose.yml" ]] || { echo "Not installed." >&2; exit 1; }; cd "${PREFIX}" && ${COMPOSE} ps; }
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
    rm -f /usr/local/bin/sounddock
  else
    echo "Data kept in ${PREFIX}/data (pass --purge to delete)."
  fi
}
cmd_doctor() {
  command -v docker
  docker compose version
  curl -fsS http://127.0.0.1:8080/healthz || true
  echo
  if [[ -f "${PREFIX}/docker-compose.yml" ]]; then
    cd "${PREFIX}" && ${COMPOSE} ps
  fi
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
