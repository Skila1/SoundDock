#!/usr/bin/env bash
# Host-side SoundDock updater. The app only writes update/request.
# systemd must exec this file only: /usr/local/lib/sounddock/host-update.sh
# This script runs on the host: classify, SQL backup if needed, docker compose pull,
# then docker compose up -d. applied is written only after digest and health.
set -euo pipefail

export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/snap/bin:${PATH:-}"
CANONICAL="ghcr.io/skila1/sounddock"

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
HEALTHY="${UPDATE}/healthy"
PROG="${UPDATE}/progress.json"
RECOVERY="${UPDATE}/needs_recovery"
mkdir -p "${UPDATE}"
chmod 02770 "${UPDATE}" 2>/dev/null || true

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

recovery_write() {
  local backup="$1" detail="$2"
  detail="${detail//\"/\\\"}"
  printf '{"status":"needs_recovery","backup":"%s","detail":"%s"}\n' "${backup}" "${detail}" > "${RECOVERY}"
  progress_write 0 "needs_recovery" "${detail}"
}

env_val() {
  local key="$1"
  if [[ -f "${PREFIX}/.env" ]]; then
    grep -E "^${key}=" "${PREFIX}/.env" | tail -1 | cut -d= -f2- | tr -d '"' || true
  fi
}

canonical_image() {
  local img="$1"
  img="${img:-${CANONICAL}:latest}"
  case "${img}" in
    "${CANONICAL}"|"${CANONICAL}:"*|"${CANONICAL}@"*) printf '%s\n' "${img}" ;;
    *) printf '%s\n' "${CANONICAL}:latest" ;;
  esac
}

schema_version() {
  local user db
  user="$(env_val POSTGRES_USER)"
  db="$(env_val POSTGRES_DB)"
  user="${user:-sounddock}"
  db="${db:-sounddock}"
  docker compose exec -T postgres psql -U "${user}" -d "${db}" -tAc "SELECT version FROM schema_migrations LIMIT 1" 2>/dev/null | tr -d '[:space:]' || true
}

image_schema_head() {
  local img="$1" head=""
  head="$(docker image inspect "${img}" --format '{{index .Config.Labels "org.sounddock.schema-head"}}' 2>/dev/null || true)"
  if [[ -z "${head}" || "${head}" == "<no value>" ]]; then
    head="$(docker run --rm --entrypoint cat "${img}" /etc/sounddock/schema-head 2>/dev/null || true)"
  fi
  head="$(echo "${head}" | tr -d '[:space:]')"
  printf '%s\n' "${head}"
}

wait_health() {
  local url="http://127.0.0.1:${SD_PORT:-8080}/healthz"
  local i
  for i in $(seq 1 60); do
    if command -v curl >/dev/null 2>&1 && curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    if command -v wget >/dev/null 2>&1 && wget -qO- "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

{
  echo "---- $(date -u +%Y-%m-%dT%H:%M:%SZ) ----"
  if [[ ! -f "${REQ}" ]]; then
    echo "no request"
    exit 0
  fi
  rm -f "${REQ}" "${HEALTHY}" "${RECOVERY}"
  if ! command -v docker >/dev/null 2>&1; then
    progress_write 0 "error" "docker not found on host PATH"
    echo "docker not found"
    exit 127
  fi
  progress_write 5 "queued" "Host received update request"
  sleep 1
  cd "${PREFIX}"
  img="$(canonical_image "$(env_val SD_IMAGE)")"
  before="$(schema_version)"
  before="${before:-0}"
  old_digest=""
  if [[ -f "${APPLIED}" ]]; then
    old_digest="$(tr -d '[:space:]' < "${APPLIED}")"
  fi

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

  target_head="$(image_schema_head "${img}")"
  target_head="${target_head:-0}"
  kind="image_only"
  if [[ "${target_head}" =~ ^[0-9]+$ ]] && (( target_head > before )); then
    kind="schema_forward"
  elif [[ -z "${target_head}" || "${target_head}" == "0" ]]; then
    kind="schema_forward"
  fi

  backup=""
  if [[ "${kind}" == "schema_forward" ]]; then
    progress_write 74 "backing_up" "Writing a SQL backup before the schema-forward apply"
    mkdir -p "${UPDATE}/backups"
    backup="${UPDATE}/backups/pre-update-$(date -u +%Y%m%d-%H%M%S).sql"
    user="$(env_val POSTGRES_USER)"; user="${user:-sounddock}"
    dbn="$(env_val POSTGRES_DB)"; dbn="${dbn:-sounddock}"
    set +e
    docker compose exec -T postgres pg_dump -U "${user}" "${dbn}" --no-owner > "${backup}"
    dump_st=$?
    set -e
    if [[ "${dump_st}" -ne 0 || ! -s "${backup}" ]]; then
      rm -f "${backup}"
      progress_write 0 "error" "SQL backup failed; schema-forward update refused"
      echo "dump failed"
      exit 1
    fi
  fi

  progress_write 80 "restarting" "Starting updated containers"
  set +e
  docker compose up -d --remove-orphans
  up_st=$?
  set -e
  after="$(schema_version)"
  after="${after:-${before}}"

  if [[ "${up_st}" -ne 0 ]] || ! wait_health; then
    if [[ "${after}" == "${before}" ]]; then
      progress_write 0 "error" "Health failed; rolling back to the previous image"
      if [[ -n "${old_digest}" ]]; then
        pin="${old_digest}"
        if [[ "${pin}" != *"@"* && "${pin}" == sha256:* ]]; then
          pin="${CANONICAL}@${pin}"
        fi
        docker tag "${pin}" "${img}" >/dev/null 2>&1 || true
        docker compose up -d --remove-orphans >/dev/null 2>&1 || true
      fi
      echo "rolled back"
      exit 1
    fi
    recovery_write "${backup}" "Schema changed and health failed. The previous image was not started. Restore from the pre-update SQL backup."
    echo "needs_recovery"
    exit 1
  fi

  digest="$(docker image inspect "${img}" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>/dev/null || true)"
  if [[ -z "${digest}" ]]; then
    progress_write 0 "error" "Could not read the pulled image digest"
    echo "missing digest"
    exit 1
  fi
  printf '%s\n' "${digest}" > "${APPLIED}"
  printf 'ok\n' > "${HEALTHY}"
  progress_write 100 "done" "Update complete"
  echo "done"
} >>"${LOG}" 2>&1
