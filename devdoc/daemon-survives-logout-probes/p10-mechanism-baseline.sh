#!/bin/bash
# p10 — the last facts the comparison table and the ladder critique need.
echo "### probe script: p10-mechanism-baseline.sh"

echo "=== 1. user@.service verbatim (Delegate= ? dependencies ?) ==="
cat /lib/systemd/system/user@.service 2>&1
echo "=== 2. delegation proof: controllers the manager subtree exposes ==="
cat /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/cgroup.controllers 2>&1
cat /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/cgroup.subtree_control 2>&1
echo "=== 3. is the USER dbus.socket unit in the manager's default.target? (usr presets) ==="
ls -l /usr/lib/systemd/user-preset/ 2>&1
grep -rn 'dbus.socket' /usr/lib/systemd/user-preset/ 2>&1 | head
echo "=== 4. linger state file (the on-disk truth for enable-linger) ==="
ls -la /var/lib/systemd/linger/ 2>&1
cat /var/lib/systemd/linger/the operator 2>&1 | head -2
echo "=== 5. KillUserProcesses / logind.conf on-disk (verbatim, effective file) ==="
grep -vE '^\s*#|^\s*$' /etc/systemd/logind.conf 2>&1
ls /etc/systemd/logind.conf.d/ 2>&1
echo "=== 6. the exact failure of every rung, re-run now (one line each) ==="
echo -n "rung1 systemd-run --user --scope : "; systemd-run --user --scope --quiet /bin/true 2>&1 | head -1
echo -n "rung1b explicit bus address     : "; DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus systemd-run --user --scope --quiet /bin/true 2>&1 | head -1
echo -n "rung2 cgroupfs: slice writable? : "; sed -n 's/.*/&/p' /dev/null; python3 -c "import os;print('access(W_OK) on user-1000.slice =', os.access('/sys/fs/cgroup/user.slice/user-1000.slice', os.W_OK))"
echo -n "rung3 setsid cgroup             : "; setsid -f true; echo "setsid ok (membership unchanged, see table)"
echo "=== 7. the manager's uptime vs boot: has it EVER restarted? ==="
echo "boot:              $(uptime -s)"
echo "user@1000 started: $(systemctl show user@1000.service -p ExecMainStartTimestamp --value 2>&1)"
echo "=== 8. how many processes a manager restart would kill ==="
cat /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/cgroup.procs 2>/dev/null | wc -l
echo "=== 9. the runtime dir is tmpfs: what happens on reboot ==="
findmnt -no SOURCE,FSTYPE,OPTIONS /run/user/1000 2>&1
echo "=== end p10 ==="
