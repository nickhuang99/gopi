#!/bin/bash
# p12 — final discriminators for the "needs root?" column and the KillUserProcesses claim.
echo "### probe script: p12-final-facts.sh"

echo "=== 1. per-user KillUserProcesses (effective, as logind reports it) ==="
loginctl show-user nick -p KillUserProcesses -p Linger -p RuntimePath -p Service 2>&1
echo "--- the same for the control user (uid 117) ---"
loginctl show-user 117 -p KillUserProcesses -p Linger -p RuntimePath -p Service 2>&1

echo "=== 2. polkit: may THIS caller enable linger for itself? (no --detail; nothing changed) ==="
pkcheck --action-id org.freedesktop.login1.set-self-linger --process 2321 2>&1; echo "set-self-linger exit=$?"
pkcheck --action-id org.freedesktop.systemd1.manage-units --process 2321 2>&1; echo "manage-units exit=$?"
pkcheck --action-id org.freedesktop.login1.set-user-linger --process 2321 2>&1; echo "set-user-linger exit=$?"

echo "=== 3. is dbus-broker (the alternative user bus) installed here? ==="
ls -l /usr/lib/systemd/user/dbus-broker.socket /usr/bin/dbus-broker 2>&1
echo "--- which dbus session implementation answers (from the running daemon) ---"
ps -o pid,cmd -p 2342 2>&1

echo "=== 4. uid 117 (control): is its bus path PRESENT in the VFS? ==="
sudo -n test -S /run/user/117/bus 2>&1; echo "(cannot read as uid 1000 — /run/user/117 is 0700, so this is a permission gap, not evidence of absence)"
ls -ld /run/user/117 2>&1

echo "=== 5. the manager's own listener fds vs the VFS for uid 1000 (the final pairing) ==="
printf 'listener fds in manager 2321 : '; ls -l /proc/2321/fd 2>/dev/null | grep -c 'socket:'
printf 'bound paths in /proc/net/unix : '; grep -c '/run/user/1000' /proc/net/unix
printf 'entries ON DISK at /run/user/1000/systemd : '; ls -1 /run/user/1000/systemd | tr '\n' ' '; echo
printf 'VFS stat of $XDG_RUNTIME_DIR/bus : '; stat -c '%F' /run/user/1000/bus 2>&1
printf 'VFS stat of $XDG_RUNTIME_DIR/systemd/private : '; stat -c '%F' /run/user/1000/systemd/private 2>&1

echo "=== 6. the session-scope survival evidence (live procs in scopes with NO current session) ==="
echo "--- current sessions (loginctl) ---"; loginctl list-sessions --no-legend 2>&1 | awk '{print $1}' | tr '\n' ' '; echo
echo "--- scopes with live procs ---"; for s in /sys/fs/cgroup/user.slice/user-1000.slice/session-*.scope; do printf '%s=%s ' "$(basename $s .scope)" "$(wc -l < $s/cgroup.procs 2>/dev/null)"; done; echo

echo "=== 7. the two sockets, one more time (stability check) ==="
grep -E '/run/user/1000/(bus|systemd/private)' /proc/net/unix 2>&1
echo "=== end p12 ==="
