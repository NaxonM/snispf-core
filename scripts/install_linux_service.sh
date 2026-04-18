#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="snispf"
INSTALL_DIR="/opt/snispf"
CONFIG_DIR="/etc/snispf"
SERVICE_PATH="/etc/systemd/system/${SERVICE_NAME}.service"

usage() {
  cat <<'EOF'
Install SNISPF as a systemd service.

Usage:
  ./install_linux_service.sh install --binary <path> --config <path>
  ./install_linux_service.sh uninstall [--purge]
  ./install_linux_service.sh status
  ./install_linux_service.sh restart
  ./install_linux_service.sh logs [--lines N]

Examples:
  sudo bash ./install_linux_service.sh install --binary ./snispf_linux_amd64 --config ./config.json
  sudo bash ./install_linux_service.sh status
  sudo bash ./install_linux_service.sh logs --lines 120
EOF
}

die() {
  echo "[snispf][error] $*" >&2
  exit 1
}

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    die "This command must run as root"
  fi
}

require_systemd() {
  command -v systemctl >/dev/null 2>&1 || die "systemctl not found"
}

install_cmd() {
  local binary_path=""
  local config_path=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --binary)
        shift
        [[ $# -gt 0 ]] || die "--binary requires a path"
        binary_path="$1"
        ;;
      --config)
        shift
        [[ $# -gt 0 ]] || die "--config requires a path"
        config_path="$1"
        ;;
      *)
        die "Unknown option for install: $1"
        ;;
    esac
    shift
  done

  [[ -n "$binary_path" ]] || die "install requires --binary <path>"
  [[ -n "$config_path" ]] || die "install requires --config <path>"
  [[ -f "$binary_path" ]] || die "Binary not found: $binary_path"
  [[ -f "$config_path" ]] || die "Config not found: $config_path"

  local script_dir
  script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
  [[ -f "${script_dir}/snispf.service" ]] || die "snispf.service not found beside installer script"

  mkdir -p "${INSTALL_DIR}" "${CONFIG_DIR}"
  cp -f "$binary_path" "${INSTALL_DIR}/snispf"
  chmod 0755 "${INSTALL_DIR}/snispf"
  cp -f "$config_path" "${CONFIG_DIR}/config.json"
  chmod 0644 "${CONFIG_DIR}/config.json"

  cp -f "${script_dir}/snispf.service" "${SERVICE_PATH}"
  chmod 0644 "${SERVICE_PATH}"

  systemctl daemon-reload
  systemctl enable --now "${SERVICE_NAME}.service"

  echo "[snispf] installed and started"
  status_cmd
}

uninstall_cmd() {
  local purge="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --purge)
        purge="true"
        ;;
      *)
        die "Unknown option for uninstall: $1"
        ;;
    esac
    shift
  done

  systemctl disable --now "${SERVICE_NAME}.service" >/dev/null 2>&1 || true
  rm -f "${SERVICE_PATH}"
  systemctl daemon-reload

  if [[ "$purge" == "true" ]]; then
    rm -rf "${INSTALL_DIR}" "${CONFIG_DIR}"
  fi

  echo "[snispf] service removed"
}

status_cmd() {
  systemctl status "${SERVICE_NAME}.service" --no-pager || true
}

restart_cmd() {
  systemctl restart "${SERVICE_NAME}.service"
  status_cmd
}

logs_cmd() {
  local lines="120"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --lines)
        shift
        [[ $# -gt 0 ]] || die "--lines requires a number"
        lines="$1"
        ;;
      *)
        die "Unknown option for logs: $1"
        ;;
    esac
    shift
  done
  journalctl -u "${SERVICE_NAME}.service" -n "$lines" --no-pager
}

main() {
  [[ $# -gt 0 ]] || {
    usage
    exit 1
  }

  local cmd="$1"
  shift || true

  case "$cmd" in
    install)
      require_root
      require_systemd
      install_cmd "$@"
      ;;
    uninstall)
      require_root
      require_systemd
      uninstall_cmd "$@"
      ;;
    status)
      require_systemd
      status_cmd
      ;;
    restart)
      require_root
      require_systemd
      restart_cmd
      ;;
    logs)
      require_systemd
      logs_cmd "$@"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      die "Unknown command: $cmd"
      ;;
  esac
}

main "$@"
