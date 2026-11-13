#!/usr/bin/env bash
# install-scratch-gc-watchdog.sh — install/refresh the host-level scratch
# watchdog for THIS user.
#
# Usage:
#   scripts/install-scratch-gc-watchdog.sh [repo-path] [--cron]
#
# Mechanism, in preference order (the epic names both):
#   1. systemd --user service + timer — the default when this host has a
#      working user manager (a socket at $XDG_RUNTIME_DIR/bus).
#   2. cron — the fallback, and the mechanism on a host where the user bus is
#      absent (a headless/server session with no graphical login: there is no
#      user@.service bus to talk to, so `systemctl --user` cannot even be
#      asked). --cron forces this path.
#
# repo-path (default: the repo this script lives in) is where the installed
# copy's ExecStart points; the watchdog script itself resolves bin/gopi there,
# so a merger rebuild (Rule 12) is picked up with no reinstall.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="${here}/.."
force_cron=0
for arg in "$@"; do
  case "${arg}" in
    --cron) force_cron=1 ;;
    *) repo="${arg}" ;;
  esac
done
repo="$(cd "${repo}" && pwd)"
unit_dir="${HOME}/.config/systemd/user"
# The SOURCE is this checkout (the installer must be runnable from the branch
# that introduces the watchdog, before it is on master); repo is only what the
# installed units are BAKED with (the checkout whose bin/gopi they should
# drive by default).
watchdog="${here}/scratch-gc-watchdog.sh"
# The STABLE runtime copy: the timer/cron line must not depend on a worktree
# (this repo's development directories come and go). Refreshed on every
# install, so the installed schedule runs the script as merged to master.
stable_dir="${HOME}/.local/bin"
stable_watchdog="${stable_dir}/gopi-scratch-gc-watchdog.sh"
# CRON_MARKER makes the crontab edit IDEMPOTENT: re-running the installer
# replaces our line instead of stacking a second sweep (a duplicate entry is a
# silent double sweep — the kind of drift a marker prevents).
CRON_MARKER="# gopi-scratch-gc-watchdog (30min, installed by scripts/install-scratch-gc-watchdog.sh)"

if [[ ! -x "${watchdog}" ]]; then
  echo "install-scratch-gc-watchdog: ${watchdog} is missing or not executable" >&2
  exit 1
fi

mkdir -p "${stable_dir}"
install -m 0755 "${watchdog}" "${stable_watchdog}"

user_bus_available() {
  [[ -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus" ]]
}

install_systemd() {
  mkdir -p "${unit_dir}"
  for unit in gopi-scratch-gc-watchdog.service gopi-scratch-gc-watchdog.timer; do
    sed "s|%h/work/GoPi|${repo}|g" "${here}/systemd/${unit}" >"${unit_dir}/${unit}"
  done
  # Point ExecStart at the stable copy (the units are installed from the repo,
  # but what they EXECUTE must outlive a worktree).
  sed -i "s|^ExecStart=.*|ExecStart=%h/.local/bin/gopi-scratch-gc-watchdog.sh|" \
    "${unit_dir}/gopi-scratch-gc-watchdog.service"
  systemctl --user daemon-reload
  systemctl --user enable --now gopi-scratch-gc-watchdog.timer
  echo "installed (systemd --user): ${unit_dir}/gopi-scratch-gc-watchdog.{service,timer}"
  systemctl --user list-timers gopi-scratch-gc-watchdog.timer --no-pager
}

install_cron() {
  command -v crontab >/dev/null 2>&1 || {
    echo "install-scratch-gc-watchdog: no crontab binary (and no user bus) — install a scheduler" >&2
    return 1
  }
  local entry="*/30 * * * * ${stable_watchdog} >/dev/null 2>&1"
  local current kept
  current="$(crontab -l 2>/dev/null || true)"
  kept="$(printf '%s\n' "${current}" | grep -v -F "${CRON_MARKER}" | grep -v -F "${stable_watchdog}" || true)"
  {
    [[ -n "${kept}" ]] && printf '%s\n' "${kept}"
    printf '%s\n' "${entry} ${CRON_MARKER}"
  } | crontab -
  echo "installed (cron): ${entry}"
  crontab -l | grep -F "${stable_watchdog}"
}

if [[ ${force_cron} -eq 1 ]] || ! user_bus_available; then
  if ! user_bus_available; then
    echo "note: no systemd --user bus at ${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus — using cron" >&2
  fi
  install_cron
else
  install_systemd
fi
