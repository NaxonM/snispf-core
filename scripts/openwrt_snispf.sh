#!/bin/sh
set -eu

APP_NAME="snispf"
INSTALL_DIR="/opt/snispf"
BIN_PATH="${INSTALL_DIR}/snispf"
CONFIG_DIR="/etc/snispf"
CONFIG_PATH="${CONFIG_DIR}/config.json"
LOG_DIR="/var/log/snispf"
LOG_FILE="${LOG_DIR}/core.log"
PID_FILE="/var/run/snispf.pid"
INIT_SCRIPT="/etc/init.d/snispf"
WATCHDOG_SCRIPT="/usr/bin/snispf-watchdog.sh"
WATCHDOG_MARKER="# SNISPF_WATCHDOG"
DEFAULT_WATCHDOG_CRON="*/1 * * * * ${WATCHDOG_SCRIPT} >/dev/null 2>&1 ${WATCHDOG_MARKER}"
DEFAULT_POST_RESTART_DELAY="20"

print_usage() {
  cat <<'EOF'
SNISPF OpenWrt deployment and management script

Usage:
  ./openwrt_snispf.sh install --binary <path> [--config <path>] [--no-enable] [--no-start] [--post-restart-delay SEC] [--no-delayed-restart] [--watchdog ask|auto|off]
  ./openwrt_snispf.sh start|stop|restart|status|enable|disable
  ./openwrt_snispf.sh logs [--follow] [--lines N]
  ./openwrt_snispf.sh monitor [--interval SEC] [--watch N]
  ./openwrt_snispf.sh watchdog-install [--schedule "cron expr"]
  ./openwrt_snispf.sh watchdog-remove
  ./openwrt_snispf.sh doctor
  ./openwrt_snispf.sh info
  ./openwrt_snispf.sh uninstall [--purge]

Examples:
  ./openwrt_snispf.sh install --binary /tmp/snispf_openwrt_armv7 --config /tmp/config.json
  ./openwrt_snispf.sh install --binary /tmp/snispf_openwrt_armv7 --watchdog auto
  ./openwrt_snispf.sh watchdog-install
  ./openwrt_snispf.sh monitor --watch 30 --interval 2
  ./openwrt_snispf.sh uninstall --purge
EOF
}

log() {
  echo "[snispf] $*"
}

warn() {
  echo "[snispf][warn] $*" >&2
}

die() {
  echo "[snispf][error] $*" >&2
  exit 1
}

prompt_yes_no_default_yes() {
  question="$1"
  printf "%s [Y/n]: " "$question"
  answer=""
  read -r answer || true
  case "$answer" in
    n|N|no|NO|No)
      return 1
      ;;
    *)
      return 0
      ;;
  esac
}

schedule_delayed_restart() {
  delay="$1"
  case "$delay" in
    ''|*[!0-9]*)
      die "Invalid delayed restart value: ${delay}"
      ;;
  esac

  [ "$delay" -gt 0 ] || return 0

  (
    sleep "$delay"
    "${INIT_SCRIPT}" restart >/dev/null 2>&1 || true
  ) >/dev/null 2>&1 &

  log "Scheduled one-time delayed restart in ${delay}s"
}

handle_watchdog_after_install() {
  mode="$1"

  case "$mode" in
    auto)
      watchdog_install_cmd
      ;;
    off)
      log "Watchdog install skipped"
      ;;
    ask)
      if [ -t 0 ] && [ -t 1 ]; then
        if prompt_yes_no_default_yes "Install watchdog for automatic recovery?"; then
          watchdog_install_cmd
        else
          log "Watchdog install skipped"
        fi
      else
        watchdog_install_cmd
      fi
      ;;
    *)
      die "Invalid watchdog mode: ${mode} (expected ask|auto|off)"
      ;;
  esac
}

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    die "This command must run as root"
  fi
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

sanitize_config_if_missing() {
  if [ ! -f "${CONFIG_PATH}" ]; then
    die "Config not found at ${CONFIG_PATH}. Install with --config first or place a valid config there."
  fi
}

extract_listen_port() {
  sanitize_config_if_missing
  sed -n 's/.*"LISTEN_PORT"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "${CONFIG_PATH}" | head -n 1
}

write_init_script() {
  cat > "${INIT_SCRIPT}" <<'EOF'
#!/bin/sh /etc/rc.common

START=95
STOP=10
USE_PROCD=1

PROG="/opt/snispf/snispf"
CONFIG="/etc/snispf/config.json"
LOG_DIR="/var/log/snispf"
LOG_FILE="${LOG_DIR}/core.log"
PID_FILE="/var/run/snispf.pid"

start_service() {
  [ -x "${PROG}" ] || {
    logger -t snispf "Binary not found at ${PROG}"
    return 1
  }
  [ -f "${CONFIG}" ] || {
    logger -t snispf "Config not found at ${CONFIG}"
    return 1
  }

  mkdir -p "${LOG_DIR}"

  procd_open_instance
  procd_set_param command /bin/sh -c "exec \"${PROG}\" --config \"${CONFIG}\" >> \"${LOG_FILE}\" 2>&1"
  procd_set_param pidfile "${PID_FILE}"
  procd_set_param respawn 3600 5 5
  procd_set_param file "${CONFIG}"
  procd_close_instance
}
EOF
  chmod 0755 "${INIT_SCRIPT}"
}

copy_binary() {
  src="$1"
  [ -f "$src" ] || die "Binary not found: $src"
  mkdir -p "${INSTALL_DIR}"
  cp -f "$src" "${BIN_PATH}"
  chmod 0755 "${BIN_PATH}"
}

copy_config() {
  src="$1"
  [ -f "$src" ] || die "Config file not found: $src"
  mkdir -p "${CONFIG_DIR}"
  cp -f "$src" "${CONFIG_PATH}"
  chmod 0644 "${CONFIG_PATH}"
}

install_cmd() {
  require_root

  binary_src=""
  config_src=""
  no_enable="0"
  no_start="0"
  watchdog_mode="ask"
  post_restart_delay="${DEFAULT_POST_RESTART_DELAY}"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --binary)
        shift
        [ "$#" -gt 0 ] || die "--binary requires a path"
        binary_src="$1"
        ;;
      --config)
        shift
        [ "$#" -gt 0 ] || die "--config requires a path"
        config_src="$1"
        ;;
      --no-enable)
        no_enable="1"
        ;;
      --no-start)
        no_start="1"
        ;;
      --watchdog)
        shift
        [ "$#" -gt 0 ] || die "--watchdog requires ask|auto|off"
        watchdog_mode="$1"
        ;;
      --no-watchdog)
        watchdog_mode="off"
        ;;
      --post-restart-delay)
        shift
        [ "$#" -gt 0 ] || die "--post-restart-delay requires seconds"
        post_restart_delay="$1"
        ;;
      --no-delayed-restart)
        post_restart_delay="0"
        ;;
      *)
        die "Unknown option for install: $1"
        ;;
    esac
    shift
  done

  [ -n "${binary_src}" ] || die "install requires --binary <path>"

  require_cmd cp
  require_cmd chmod
  [ -x "/etc/init.d/cron" ] || die "Missing /etc/init.d/cron"

  copy_binary "${binary_src}"
  mkdir -p "${CONFIG_DIR}" "${LOG_DIR}"

  if [ -n "${config_src}" ]; then
    copy_config "${config_src}"
  elif [ ! -f "${CONFIG_PATH}" ]; then
    die "No config installed yet. Provide --config <path> for first install."
  fi

  write_init_script

  if [ "${no_enable}" = "0" ]; then
    "${INIT_SCRIPT}" enable
  fi

  if [ "${no_start}" = "0" ]; then
    if "${INIT_SCRIPT}" running >/dev/null 2>&1; then
      "${INIT_SCRIPT}" restart
    else
      "${INIT_SCRIPT}" start
    fi
    schedule_delayed_restart "${post_restart_delay}"
  fi

  handle_watchdog_after_install "${watchdog_mode}"

  log "Install complete"
  log "Binary: ${BIN_PATH}"
  log "Config: ${CONFIG_PATH}"
  status_cmd
}

start_cmd() {
  require_root
  "${INIT_SCRIPT}" start
}

stop_cmd() {
  require_root
  "${INIT_SCRIPT}" stop
}

restart_cmd() {
  require_root
  "${INIT_SCRIPT}" restart
}

enable_cmd() {
  require_root
  "${INIT_SCRIPT}" enable
}

disable_cmd() {
  require_root
  "${INIT_SCRIPT}" disable
}

status_cmd() {
  if [ -x "${INIT_SCRIPT}" ]; then
    if "${INIT_SCRIPT}" enabled >/dev/null 2>&1; then
      echo "enabled=yes"
    else
      echo "enabled=no"
    fi

    if "${INIT_SCRIPT}" running >/dev/null 2>&1; then
      echo "running=yes"
    else
      echo "running=no"
    fi
  else
    echo "installed=no"
    return 1
  fi

  if [ -f "${PID_FILE}" ]; then
    pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
    if [ -n "${pid}" ]; then
      echo "pid=${pid}"
    fi
  fi

  port="$(extract_listen_port 2>/dev/null || true)"
  if [ -n "${port}" ]; then
    echo "listen_port=${port}"
    if netstat -lnt 2>/dev/null | grep -q ":${port}[[:space:]]"; then
      echo "port_listening=yes"
    else
      echo "port_listening=no"
    fi
  fi
}

logs_cmd() {
  follow="0"
  lines="120"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --follow)
        follow="1"
        ;;
      --lines)
        shift
        [ "$#" -gt 0 ] || die "--lines requires a number"
        lines="$1"
        ;;
      *)
        die "Unknown option for logs: $1"
        ;;
    esac
    shift
  done

  [ -f "${LOG_FILE}" ] || die "Log file not found: ${LOG_FILE}"

  if [ "${follow}" = "1" ]; then
    tail -n "${lines}" -f "${LOG_FILE}"
  else
    tail -n "${lines}" "${LOG_FILE}"
  fi
}

monitor_snapshot() {
  timestamp="$(date '+%Y-%m-%d %H:%M:%S')"
  echo "=== ${timestamp} ==="
  if ! status_cmd; then
    echo "status=not_installed"
  fi

  if [ -x "${BIN_PATH}" ]; then
    info_line="$("${BIN_PATH}" --info 2>/dev/null | grep '^af_packet=' || true)"
    if [ -n "${info_line}" ]; then
      echo "${info_line}"
    fi
  fi

  echo "log_tail:"
  if [ -f "${LOG_FILE}" ]; then
    tail -n 8 "${LOG_FILE}" | sed 's/^/  /'
  else
    echo "  no-log-file"
  fi
}

monitor_cmd() {
  interval="3"
  watch_count="0"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --interval)
        shift
        [ "$#" -gt 0 ] || die "--interval requires seconds"
        interval="$1"
        ;;
      --watch)
        shift
        [ "$#" -gt 0 ] || die "--watch requires count"
        watch_count="$1"
        ;;
      *)
        die "Unknown option for monitor: $1"
        ;;
    esac
    shift
  done

  n=0
  while :; do
    monitor_snapshot
    n=$((n + 1))

    if [ "${watch_count}" -gt 0 ] && [ "$n" -ge "${watch_count}" ]; then
      break
    fi

    sleep "${interval}"
  done
}

write_watchdog_script() {
  cat > "${WATCHDOG_SCRIPT}" <<'EOF'
#!/bin/sh
set -eu

INIT_SCRIPT="/etc/init.d/snispf"
CONFIG_PATH="/etc/snispf/config.json"
LOG_FILE="/var/log/snispf/core.log"

log() {
  logger -t snispf-watchdog "$*"
}

extract_port() {
  sed -n 's/.*"LISTEN_PORT"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "${CONFIG_PATH}" | head -n 1
}

if [ ! -x "${INIT_SCRIPT}" ]; then
  exit 0
fi

if ! "${INIT_SCRIPT}" enabled >/dev/null 2>&1; then
  exit 0
fi

if ! "${INIT_SCRIPT}" running >/dev/null 2>&1; then
  log "Service not running, restarting"
  "${INIT_SCRIPT}" restart || true
  exit 0
fi

if [ -f "${CONFIG_PATH}" ]; then
  port="$(extract_port || true)"
  if [ -n "${port}" ]; then
    if ! netstat -lnt 2>/dev/null | grep -q ":${port}[[:space:]]"; then
      log "Service running but port ${port} not listening, restarting"
      "${INIT_SCRIPT}" restart || true
      exit 0
    fi
  fi
fi

if [ -f "${LOG_FILE}" ]; then
  if tail -n 60 "${LOG_FILE}" | grep -qi "panic\|fatal\|segmentation fault\|raw injector unavailable at runtime\|wrong_seq requires raw injector support\|raw injector route-change detected"; then
    log "Detected fatal/degraded runtime pattern in log tail, restarting"
    "${INIT_SCRIPT}" restart || true
    exit 0
  fi
fi
EOF
  chmod 0755 "${WATCHDOG_SCRIPT}"
}

watchdog_install_cmd() {
  require_root

  schedule="${DEFAULT_WATCHDOG_CRON}"
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --schedule)
        shift
        [ "$#" -gt 0 ] || die "--schedule requires a cron expression"
        schedule="$1 ${WATCHDOG_MARKER}"
        ;;
      *)
        die "Unknown option for watchdog-install: $1"
        ;;
    esac
    shift
  done

  write_watchdog_script

  tmp_cron="$(mktemp)"
  crontab -l 2>/dev/null | grep -v "${WATCHDOG_MARKER}" > "${tmp_cron}" || true
  echo "${schedule}" >> "${tmp_cron}"
  crontab "${tmp_cron}"
  rm -f "${tmp_cron}"

  /etc/init.d/cron restart >/dev/null 2>&1 || true
  log "Watchdog installed"
}

watchdog_remove_cmd() {
  require_root

  tmp_cron="$(mktemp)"
  crontab -l 2>/dev/null | grep -v "${WATCHDOG_MARKER}" > "${tmp_cron}" || true
  crontab "${tmp_cron}" || true
  rm -f "${tmp_cron}"

  rm -f "${WATCHDOG_SCRIPT}"
  /etc/init.d/cron restart >/dev/null 2>&1 || true
  log "Watchdog removed"
}

doctor_cmd() {
  echo "== Binary =="
  if [ -x "${BIN_PATH}" ]; then
    echo "ok: ${BIN_PATH}"
  else
    echo "missing: ${BIN_PATH}"
  fi

  echo "== Config =="
  if [ -f "${CONFIG_PATH}" ]; then
    echo "ok: ${CONFIG_PATH}"
  else
    echo "missing: ${CONFIG_PATH}"
  fi

  echo "== Service =="
  if [ -x "${INIT_SCRIPT}" ]; then
    echo "ok: ${INIT_SCRIPT}"
  else
    echo "missing: ${INIT_SCRIPT}"
  fi

  echo "== Status =="
  status_cmd || true

  echo "== Raw capability =="
  if [ -x "${BIN_PATH}" ]; then
    "${BIN_PATH}" --info 2>/dev/null | grep -E '^(raw_socket|af_packet|raw_injection_diagnostic)=' || true
  fi
}

info_cmd() {
  [ -x "${BIN_PATH}" ] || die "Binary not found at ${BIN_PATH}"
  "${BIN_PATH}" --info
}

uninstall_cmd() {
  require_root
  purge="0"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --purge)
        purge="1"
        ;;
      *)
        die "Unknown option for uninstall: $1"
        ;;
    esac
    shift
  done

  if [ -x "${INIT_SCRIPT}" ]; then
    "${INIT_SCRIPT}" stop >/dev/null 2>&1 || true
    "${INIT_SCRIPT}" disable >/dev/null 2>&1 || true
  fi

  watchdog_remove_cmd

  rm -f "${INIT_SCRIPT}"

  if [ "${purge}" = "1" ]; then
    rm -rf "${INSTALL_DIR}" "${CONFIG_DIR}" "${LOG_DIR}"
    log "Removed binary, config, and logs"
  else
    rm -f "${BIN_PATH}"
    log "Removed binary only (config/logs kept). Use --purge for full cleanup."
  fi

  log "Uninstall complete"
}

main() {
  if [ "$#" -lt 1 ]; then
    print_usage
    exit 1
  fi

  cmd="$1"
  shift

  case "$cmd" in
    install)
      install_cmd "$@"
      ;;
    start)
      start_cmd
      ;;
    stop)
      stop_cmd
      ;;
    restart)
      restart_cmd
      ;;
    status)
      status_cmd
      ;;
    enable)
      enable_cmd
      ;;
    disable)
      disable_cmd
      ;;
    logs)
      logs_cmd "$@"
      ;;
    monitor)
      monitor_cmd "$@"
      ;;
    watchdog-install)
      watchdog_install_cmd "$@"
      ;;
    watchdog-remove)
      watchdog_remove_cmd
      ;;
    doctor)
      doctor_cmd
      ;;
    info)
      info_cmd
      ;;
    uninstall)
      uninstall_cmd "$@"
      ;;
    help|-h|--help)
      print_usage
      ;;
    *)
      die "Unknown command: $cmd"
      ;;
  esac
}

main "$@"
