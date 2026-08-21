#!/usr/bin/env bash
# SoundDock installer. Whiptail TUI (Proxmox helper style). Do not apt install docker.
#
#   sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/Skila1/SoundDock/main/install.sh)"
#
# After install: sudo sounddock status|update|logs|uninstall|doctor
# Unattended:    sudo env SD_UNATTENDED=1 bash -c "$(curl -fsSL ...)"
#
set -euo pipefail

# Install into ./sounddock under the current directory (or here, if this folder is already named sounddock).
prefix_from_here() {
  local cwd
  cwd="$(pwd -L)"
  if [[ "$(basename "${cwd}")" == "sounddock" ]]; then
    printf '%s\n' "${cwd}"
  else
    printf '%s\n' "${cwd%/}/sounddock"
  fi
}
PREFIX="$(prefix_from_here)"
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

install_cloudflared_pkg() {
  if command -v cloudflared >/dev/null 2>&1; then
    return
  fi
  msg_info "Installing cloudflared"
  local os arch deb rpm bin
  os="$(detect_os)"
  arch="$(uname -m)"
  case "$arch" in
    aarch64|arm64) deb="cloudflared-linux-arm64.deb"; rpm="cloudflared-linux-aarch64.rpm"; bin="cloudflared-linux-arm64" ;;
    *) deb="cloudflared-linux-amd64.deb"; rpm="cloudflared-linux-x86_64.rpm"; bin="cloudflared-linux-amd64" ;;
  esac
  case "$os" in
    ubuntu|debian)
      curl -fsSL -o /tmp/cloudflared.deb "https://github.com/cloudflare/cloudflared/releases/latest/download/${deb}"
      dpkg -i /tmp/cloudflared.deb || apt-get install -y -f
      rm -f /tmp/cloudflared.deb
      ;;
    fedora|rhel|centos)
      curl -fsSL -o /tmp/cloudflared.rpm "https://github.com/cloudflare/cloudflared/releases/latest/download/${rpm}"
      rpm -i /tmp/cloudflared.rpm || dnf install -y /tmp/cloudflared.rpm || yum install -y /tmp/cloudflared.rpm
      rm -f /tmp/cloudflared.rpm
      ;;
    *)
      curl -fsSL -o /usr/local/bin/cloudflared "https://github.com/cloudflare/cloudflared/releases/latest/download/${bin}"
      chmod 0755 /usr/local/bin/cloudflared
      ;;
  esac
  if ! command -v cloudflared >/dev/null 2>&1; then
    msg_err "cloudflared is not on PATH after install."
    exit 1
  fi
  msg_ok "cloudflared installed"
}

install_cloudflared_service() {
  local token="${1:-}"
  if cloudflared_service_present; then
    msg_ok "cloudflared systemd service already exists, leaving it as-is"
    return
  fi
  if [[ -z "${token}" ]]; then
    return
  fi
  install_cloudflared_pkg
  if [[ -f "${PREFIX}/docker-compose.yml" ]]; then
    (cd "${PREFIX}" && ${COMPOSE} stop cloudflared >/dev/null 2>&1 || true)
    (cd "${PREFIX}" && ${COMPOSE} rm -f cloudflared >/dev/null 2>&1 || true)
  fi
  msg_info "Installing cloudflared systemd service"
  cloudflared service install "${token}"
  systemctl enable --now cloudflared
  msg_ok "cloudflared is running as a systemd service (origin: http://localhost:8080)"
}

cloudflared_service_present() {
  command -v systemctl >/dev/null 2>&1 || return 1
  if systemctl is-active --quiet cloudflared 2>/dev/null; then
    return 0
  fi
  if systemctl is-enabled --quiet cloudflared 2>/dev/null; then
    return 0
  fi
  systemctl list-unit-files --type=service --no-legend 2>/dev/null | grep -q '^cloudflared\.service'
}

rand() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 36 | tr -d '\n/=+' | head -c 48
  else
    tr -dc 'A-Za-z0-9' </dev/urandom | head -c 48
  fi
}

# Filled by collect_config
CFG_LIBHOST=""
CFG_CFTOK=""

collect_config() {
  PREFIX="$(prefix_from_here)"
  CFG_LIBHOST="${PREFIX}/libraries"
  CFG_CFTOK="${CLOUDFLARE_TUNNEL_TOKEN:-}"

  if ! ui_ok; then
    msg_info "No TUI (set SD_UNATTENDED=1 or no /dev/tty). Installing in ${PREFIX}"
    return
  fi

  ui_msg "SoundDock" "Use Tab and Enter to move.\n\nInstalls into a sounddock folder in this directory.\nIf you are already in a folder named sounddock, it installs here.\nDocker publishes port 8080. Cloudflare Tunnel (optional) is a systemd service. Discord is configured later in Admin." 14 || true

  if ! ui_yesno "Install SoundDock" "Install here:\n${PREFIX}\n\nWrites docker-compose.yml and .env.\nPort 8080 on the host.\nThen: cd ${PREFIX} && docker compose ps"; then
    echo "Cancelled."
    exit 0
  fi

  if cloudflared_service_present; then
    msg_ok "cloudflared is already installed as a systemd service"
  elif ui_yesno "Cloudflare Tunnel" "Install cloudflared as a systemd service now?\n\nNot Docker. Point the tunnel at http://localhost:8080 (Docker publishes that port on the host)." 13; then
    CFG_CFTOK="$(ui_pass "Tunnel token" "Cloudflare Tunnel token from Zero Trust.")" || exit 0
  fi

  local cfstatus="no"
  if cloudflared_service_present; then
    cfstatus="already installed"
  elif [[ -n "${CFG_CFTOK}" ]]; then
    cfstatus="yes"
  fi
  local summary
  summary="Compose project: ${PREFIX}\nFiles: docker-compose.yml and .env\nHost port: 8080\nLibraries: ${CFG_LIBHOST}\ncloudflared systemd: ${cfstatus}\n\nFirst visit: create a local administrator in the browser.\nDiscord is set up later under Admin."
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
    \${COMPOSE} up -d --remove-orphans
    ;;
  uninstall)
    need_root
    cd_prefix
    if [[ "\${2:-}" == "--purge" ]]; then
      \${COMPOSE} down -v
      rm -rf "\${PREFIX}"
      rm -f /usr/local/bin/sounddock
      systemctl disable --now sounddock-update.path >/dev/null 2>&1 || true
      rm -f /etc/systemd/system/sounddock-update.service /etc/systemd/system/sounddock-update.path /usr/local/lib/sounddock/update.sh /usr/local/lib/sounddock/host-update.sh
      systemctl daemon-reload >/dev/null 2>&1 || true
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
    else
      echo "Missing \${PREFIX}/docker-compose.yml"
    fi
    if command -v cloudflared >/dev/null 2>&1; then
      systemctl is-active cloudflared || true
    fi
    if command -v systemctl >/dev/null 2>&1; then
      systemctl is-enabled sounddock-update.path 2>/dev/null || true
      systemctl is-active sounddock-update.path 2>/dev/null || true
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

install_update_helper() {
  need_root
  mkdir -p "${PREFIX}/update" /usr/local/lib/sounddock
  chmod 0777 "${PREFIX}/update" || true
  printf '1\n' > "${PREFIX}/update/helper"
  cat > /usr/local/lib/sounddock/host-update.sh <<'HOST'
#!/usr/bin/env bash
# Host-side SoundDock updater. The app only writes update/request.
# This script runs on the host: docker compose pull, then docker compose up -d.
set -euo pipefail

export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/snap/bin:${PATH:-}"

if [[ -n "${SD_UPDATE_PREFIX:-}" ]]; then
  PREFIX="${SD_UPDATE_PREFIX}"
elif [[ "$(basename "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)")" == "update" ]]; then
  PREFIX="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
else
  echo "SD_UPDATE_PREFIX is not set" >&2
  exit 1
fi

UPDATE="${PREFIX}/update"
REQ="${UPDATE}/request"
LOG="${UPDATE}/last.log"
APPLIED="${UPDATE}/applied"
PROG="${UPDATE}/progress.json"
mkdir -p "${UPDATE}"

exec 9>"${UPDATE}/.lock"
if command -v flock >/dev/null 2>&1; then
  if ! flock -n 9; then
    echo "update already running" >>"${LOG}"
    exit 0
  fi
fi

progress_write() {
  local percent="$1" stage="$2" detail="$3"
  detail="${detail//$'\r'/}"
  detail="${detail//\\/\\\\}"
  detail="${detail//\"/\\\"}"
  detail="${detail//$'\n'/ }"
  printf '{"percent":%s,"stage":"%s","detail":"%s"}\n' "${percent}" "${stage}" "${detail}" > "${PROG}.tmp"
  mv -f "${PROG}.tmp" "${PROG}"
}

{
  echo "---- $(date -u +%Y-%m-%dT%H:%M:%SZ) ----"
  if [[ ! -f "${REQ}" ]]; then
    echo "no request"
    exit 0
  fi
  rm -f "${REQ}"
  if ! command -v docker >/dev/null 2>&1; then
    progress_write 0 "error" "docker not found on host PATH"
    echo "docker not found"
    exit 127
  fi
  progress_write 5 "queued" "Host received update request"
  sleep 1
  cd "${PREFIX}"
  img=""
  if [[ -f .env ]]; then
    img="$(grep -E '^SD_IMAGE=' .env | tail -1 | cut -d= -f2- | tr -d '"' || true)"
  fi
  img="${img:-ghcr.io/skila1/sounddock:latest}"

  progress_write 10 "pulling" "Pulling ${img}"
  set +e
  docker compose pull >>"${LOG}" 2>&1 &
  pull_pid=$!
  pct=10
  while kill -0 "${pull_pid}" 2>/dev/null; do
    sleep 2
    if (( pct < 72 )); then
      pct=$((pct + 2))
    fi
    last="$(tail -n 1 "${LOG}" 2>/dev/null | tr -d '\r' || true)"
    if [[ "${last}" =~ ([0-9]{1,3})% ]]; then
      mapped=$((10 + BASH_REMATCH[1] * 62 / 100))
      if (( mapped > pct )); then
        pct="${mapped}"
      fi
    fi
    progress_write "${pct}" "pulling" "${last:-Downloading layers}"
  done
  wait "${pull_pid}"
  pull_st=$?
  set -e
  if [[ "${pull_st}" -ne 0 ]]; then
    progress_write 0 "error" "Image pull failed"
    echo "pull failed: ${pull_st}"
    exit "${pull_st}"
  fi

  progress_write 80 "restarting" "Starting updated containers"
  docker compose up -d --remove-orphans
  digest="$(docker image inspect "${img}" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>/dev/null || true)"
  if [[ -n "${digest}" ]]; then
    printf '%s\n' "${digest}" > "${APPLIED}"
  fi
  progress_write 100 "done" "Update complete"
  echo "done"
} >>"${LOG}" 2>&1
HOST
  chmod 0755 /usr/local/lib/sounddock/host-update.sh
  cat > /usr/local/lib/sounddock/update.sh <<EOF
#!/usr/bin/env bash
set -euo pipefail
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/snap/bin:\${PATH:-}"
export SD_UPDATE_PREFIX="${PREFIX}"
if [[ -f "${PREFIX}/update/run.sh" ]]; then
  exec /bin/bash "${PREFIX}/update/run.sh"
fi
exec /bin/bash /usr/local/lib/sounddock/host-update.sh
EOF
  chmod 0755 /usr/local/lib/sounddock/update.sh
  cat > /etc/systemd/system/sounddock-update.service <<EOF
[Unit]
Description=SoundDock host image update
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
WorkingDirectory=${PREFIX}
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/snap/bin
Environment=SD_UPDATE_PREFIX=${PREFIX}
ExecStart=/usr/local/lib/sounddock/update.sh
Nice=5
TimeoutStartSec=30min
EOF
  cat > /etc/systemd/system/sounddock-update.path <<EOF
[Unit]
Description=Watch for SoundDock update requests

[Path]
PathExists=${PREFIX}/update/request
PathChanged=${PREFIX}/update/request
PathModified=${PREFIX}/update/request
Unit=sounddock-update.service
MakeDirectory=true

[Install]
WantedBy=multi-user.target
EOF
  cat > /etc/systemd/system/sounddock-update.timer <<EOF
[Unit]
Description=Poll SoundDock update requests (inotify misses container bind-mount writes)

[Timer]
OnBootSec=20s
OnUnitInactiveSec=5s
AccuracySec=1s
Unit=sounddock-update.service

[Install]
WantedBy=timers.target
EOF
  systemctl daemon-reload
  systemctl enable --now sounddock-update.path
  systemctl enable --now sounddock-update.timer
  msg_ok "Host update helper enabled (sounddock-update.path + timer)"
}

remove_update_helper() {
  if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now sounddock-update.path >/dev/null 2>&1 || true
    systemctl disable --now sounddock-update.timer >/dev/null 2>&1 || true
    systemctl stop sounddock-update.service >/dev/null 2>&1 || true
  fi
  rm -f /etc/systemd/system/sounddock-update.service /etc/systemd/system/sounddock-update.path /etc/systemd/system/sounddock-update.timer /usr/local/lib/sounddock/update.sh /usr/local/lib/sounddock/host-update.sh
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
  fi
}

write_compose() {
  mkdir -p "${PREFIX}"
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
      SD_COMPOSE_PROJECT: sounddock
      SD_UPDATE_DIR: /update
      SD_DATABASE_URL: postgres://\${POSTGRES_USER:-sounddock}:\${POSTGRES_PASSWORD}@postgres:5432/\${POSTGRES_DB:-sounddock}?sslmode=disable
    group_add:
      - "\${SD_DOCKER_GID:-0}"
    volumes:
      - sounddock_data:/data
      - \${SD_LIBRARY_HOST:-./libraries}:/libraries:ro
      - ./update:/update
      - /var/run/docker.sock:/var/run/docker.sock
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

volumes:
  postgres_data:
  sounddock_data:

networks:
  sounddock:
    driver: bridge
EOF
  chmod 0644 "${PREFIX}/docker-compose.yml"
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
  PREFIX="$(prefix_from_here)"
  CFG_LIBHOST="${PREFIX}/libraries"

  install_docker
  mkdir -p "${PREFIX}/data/cache" "${PREFIX}/data/backups" "${PREFIX}/data/managed" "${PREFIX}/libraries" "${PREFIX}/update"
  chmod 0777 "${PREFIX}/update" || true
  mkdir -p "${CFG_LIBHOST}"
  write_compose
  write_cli
  save_installer
  install_update_helper

  local mk pw
  mk="$(rand)"
  pw="$(rand)"
  local cookie="false"
  if [[ -n "${CFG_CFTOK}" ]] || cloudflared_service_present; then
    cookie="true"
  fi
  local dockergid="0"
  if [[ -S /var/run/docker.sock ]]; then
    dockergid="$(stat -c %g /var/run/docker.sock 2>/dev/null || echo 0)"
  fi
  if [[ -f "${PREFIX}/.env" ]] && grep -q SD_MASTER_KEY "${PREFIX}/.env"; then
    msg_info "Keeping existing ${PREFIX}/.env"
    grep -q '^SD_IMAGE=' "${PREFIX}/.env" || echo "SD_IMAGE=${IMAGE}" >> "${PREFIX}/.env"
    grep -q '^SD_LIBRARY_HOST=' "${PREFIX}/.env" || echo "SD_LIBRARY_HOST=${CFG_LIBHOST}" >> "${PREFIX}/.env"
    grep -q '^SD_DOCKER_GID=' "${PREFIX}/.env" || echo "SD_DOCKER_GID=${dockergid}" >> "${PREFIX}/.env"
    grep -q '^SD_COMPOSE_PROJECT=' "${PREFIX}/.env" || echo "SD_COMPOSE_PROJECT=sounddock" >> "${PREFIX}/.env"
  else
    cat > "${PREFIX}/.env" <<EOF
SD_ROLE=all
SD_HTTP_ADDR=:8080
SD_INSTANCE_NAME=SoundDock
SD_DATABASE_URL=postgres://sounddock:${pw}@postgres:5432/sounddock?sslmode=disable
SD_MASTER_KEY=${mk}
SD_DATA_DIR=/data
SD_CACHE_DIR=/data/cache
SD_BACKUP_DIR=/data/backups
SD_MANAGED_DIR=/data/managed
SD_COOKIE_SECURE=${cookie}
SD_TRUSTED_PROXIES=127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16
SD_IMAGE=${IMAGE}
SD_LIBRARY_HOST=${CFG_LIBHOST}
SD_PORT=8080
SD_DOCKER_GID=${dockergid}
SD_COMPOSE_PROJECT=sounddock
POSTGRES_USER=sounddock
POSTGRES_PASSWORD=${pw}
POSTGRES_DB=sounddock
EOF
    chmod 0600 "${PREFIX}/.env"
  fi

  if [[ ! -f "${PREFIX}/docker-compose.yml" || ! -f "${PREFIX}/.env" ]]; then
    msg_err "Failed to write ${PREFIX}/docker-compose.yml and ${PREFIX}/.env"
    exit 1
  fi
  msg_ok "Wrote ${PREFIX}/docker-compose.yml and ${PREFIX}/.env"

  if grep -q '^COMPOSE_PROFILES=.*cloudflare' "${PREFIX}/.env" 2>/dev/null; then
    sed -i.bak -e '/^COMPOSE_PROFILES=/d' "${PREFIX}/.env" || true
  fi

  cd "${PREFIX}"
  msg_info "Pulling ${IMAGE} in ${PREFIX}"
  ${COMPOSE} pull
  ${COMPOSE} up -d
  digest="$(docker image inspect "${IMAGE}" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>/dev/null || true)"
  if [[ -n "${digest}" ]]; then
    printf '%s\n' "${digest}" > "${PREFIX}/update/applied"
  fi
  msg_ok "SoundDock is starting"

  local i
  for i in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done

  install_cloudflared_service "${CFG_CFTOK}"

  local extra=""
  if [[ -n "${CFG_CFTOK}" ]]; then
    extra="\ncloudflared: systemd (sudo systemctl status cloudflared)\nTunnel origin: http://localhost:8080"
  fi
  local done="Compose project: ${PREFIX}\n  docker-compose.yml\n  .env\nHost port: 8080${extra}\n\nOpen http://<this-host>:8080 and create the first administrator.\n\ncd ${PREFIX}\ndocker compose ps"
  if ui_ok; then
    ui_msg "Installed" "${done}" 18 || true
  fi
  echo
  echo -e " ${BOLD}${BL}SoundDock files are in ${PREFIX}${CL}"
  echo "  ${PREFIX}/docker-compose.yml"
  echo "  ${PREFIX}/.env"
  echo "  Open http://<this-host>:8080 and create the first administrator."
  echo "  Discord is configured in Admin after you log in."
  if [[ -n "${CFG_CFTOK}" ]]; then
    echo "  cloudflared: systemctl status cloudflared"
  fi
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
    remove_update_helper
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
  else
    echo "Missing ${PREFIX}/docker-compose.yml"
  fi
  if command -v cloudflared >/dev/null 2>&1; then
    systemctl is-active cloudflared || true
  fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl is-enabled sounddock-update.path 2>/dev/null || echo "sounddock-update.path: not enabled"
    systemctl is-enabled sounddock-update.timer 2>/dev/null || echo "sounddock-update.timer: not enabled"
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
