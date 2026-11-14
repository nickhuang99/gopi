#!/bin/bash
# p9 — repair-path feasibility + unit inventory. Read-only: nothing started/restarted.
echo "### probe script: p9-repair-feasibility.sh"

echo "=== 1. user dbus units shipped + their activation state ==="
ls -l /usr/lib/systemd/user/dbus.socket /usr/lib/systemd/user/dbus.service 2>&1
systemctl is-enabled dbus.socket 2>&1
systemctl status dbus.socket --no-pager 2>&1 | head -8
echo "--- was the user manager EVER asked to start it this boot (uid 1000) ---"
journalctl --no-pager -b 2>/dev/null | grep -cE 'D-Bus User Message Bus'

echo "=== 2. dbus-daemon 2342: cgroup, start reference, what started it ==="
cat /proc/2342/cgroup 2>&1
ps -o pid,ppid,lstart,cmd -p 2342 2>&1
echo "--- its parent ---"
awk '{print "ppid="$4}' /proc/2342/stat 2>&1
echo "--- its command line ---"
tr '\0' ' ' < /proc/2342/cmdline 2>&1; echo
echo "--- its own environment (bus address it advertises) ---"
tr '\0' '\n' < /proc/2342/environ 2>&1 | grep -E 'DBUS|XDG|DISPLAY' | head

echo "=== 3. did the bus advertise an address anywhere durable? ==="
cat /run/user/1000/dbus-1/machine-id 2>&1
ls -la /run/user/1000/dbus-1/ 2>&1
tr '\0' '\n' < /proc/2321/environ 2>&1 | grep -cE 'DBUS_SESSION_BUS_ADDRESS'

echo "=== 4. is a restart of user@1000.service permitted WITHOUT root? (pkcheck, no action taken) ==="
command -v pkcheck 2>&1
pkcheck --action-id org.freedesktop.systemd1.manage-units --process 2321 --detail unit user@1000.service 2>&1; echo "pkcheck(user@1000 unit manage) exit=$?"
pkcheck --action-id org.freedesktop.systemd1.manage-units --process 2321 --detail unit user-runtime-dir@1000.service 2>&1; echo "pkcheck(user-runtime-dir@1000) exit=$?"

echo "=== 5. what a restart would cost (units under the manager, count only) ==="
find /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service -maxdepth 2 -name '*.service' -o -maxdepth 2 -name '*.scope' 2>/dev/null | wc -l

echo "=== 6. session scopes with live processes (logout-survival evidence) ==="
for s in /sys/fs/cgroup/user.slice/user-1000.slice/session-*.scope; do
  n=$(cat "$s/cgroup.procs" 2>/dev/null | wc -l)
  printf '%s procs=%s\n' "$(basename "$s")" "$n"
done

echo "=== 7. the verification instruments themselves (present? usable?) ==="
for c in systemd-run busctl loginctl systemd-cgls systemctl setsid nohup screen tmux unshare; do
  printf '%-12s %s\n' "$c" "$(command -v $c 2>/dev/null || echo ABSENT)"
done
echo "=== end p9 ==="
