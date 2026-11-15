#!/bin/sh
# P1 — host identity + logind/linger state. Read-only.
sec() { echo; echo "=== $1 ==="; }
echo "### probe script: p1-identity-logind.sh"
sec "date"; date -Is
sec "uname -a"; uname -a
sec "os-release"; cat /etc/os-release
sec "systemd --version"; systemd --version
sec "id"; id
sec "/proc/self/cgroup"; cat /proc/self/cgroup
sec "loginctl show-user the operator"; loginctl show-user the operator
sec "loginctl list-sessions"; loginctl list-sessions
sec "effective /etc/systemd/logind.conf (comments stripped)"; grep -vE '^[[:space:]]*(#|$)' /etc/systemd/logind.conf
sec "logind.conf.d dir"; ls -la /etc/systemd/logind.conf.d/ 2>&1
sec "logind.conf.d contents"; for f in /etc/systemd/logind.conf.d/*.conf; do [ -e "$f" ] && { echo "--- $f"; cat "$f"; }; done 2>&1
sec "systemctl show systemd-logind -p KillUserProcesses,RemoveIPC,IdleAction"; systemctl show systemd-logind -p KillUserProcesses -p RemoveIPC -p IdleAction -p IdleActionUSec 2>&1
sec "systemctl show user@1000.service"; systemctl show user@1000.service
sec "XDG_RUNTIME_DIR / /run/user/1000"; echo "XDG_RUNTIME_DIR=[${XDG_RUNTIME_DIR-<unset>}]"; ls -ld /run/user/1000
sec "systemctl status user@1000.service (head)"; systemctl status user@1000.service 2>&1 | head -12
sec "utmp/who"; who 2>&1
