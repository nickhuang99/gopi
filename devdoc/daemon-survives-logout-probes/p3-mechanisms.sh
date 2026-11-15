#!/bin/sh
# P3 — each candidate mechanism, RUN (not predicted). Read-only except one
# transient cgroup dir + one /bin/true scope, both removed/self-exiting.
sec() { echo; echo "=== $1 ==="; }
echo "### probe script: p3-mechanisms.sh"
UID_N=$(id -u)
sec "systemd-run --user --scope (the declarative escape)"; /usr/bin/systemd-run --user --scope --quiet /bin/true 2>&1; echo "exit=$?"
sec "systemd-run --user --scope with explicit DBUS_SESSION_BUS_ADDRESS"; DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$UID_N/bus" /usr/bin/systemd-run --user --scope --quiet /bin/true 2>&1; echo "exit=$?"
sec "systemctl --user status"; systemctl --user status 2>&1 | head -3; echo "exit=$?"
sec "busctl --user list"; busctl --user list 2>&1 | head -3; echo "exit=$?"
sec "systemd-run --scope (SYSTEM bus, no --user) — transient system scope"; /usr/bin/systemd-run --scope --quiet --unit=gopi-probe-readonly-$UID_N /bin/true 2>&1; echo "exit=$?"
sec "systemctl start of a transient unit (system bus, polkit path) — SKIPPED: a polkit-gated start can raise an interactive auth dialog on the operator's desktop; the systemd-run --scope probe above exercises the same privileged path"
sec "loginctl enable-linger (already set?) and terminate-user (NOT run)"; echo "linger state above; 'loginctl terminate-user the operator' deliberately NOT run (kills every session)"
sec "cgroupfs: per-uid slice writability"; S=/sys/fs/cgroup/user.slice/user-1000.slice; ls -ld "$S"; test -w "$S" && echo "test -w $S: YES" || echo "test -w $S: NO"
sec "access(2) W_OK on the common ancestor (what the kernel gates on)"; python3 - <<'PY' 2>&1
import os
for p in ["/sys/fs/cgroup/user.slice/user-1000.slice",
          "/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service",
          "/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/cgroup.procs",
          "/sys/fs/cgroup/user.slice/user-1000.slice/cgroup.procs",
          "/sys/fs/cgroup/cgroup.procs"]:
    print(f"os.access({p}, W_OK) = {os.access(p, os.W_OK)}  exists={os.path.exists(p)}")
PY
sec "cgroupfs: the REAL migration attempt (mkdir + write own pid + fresh read) — transient, removed after"; MGR=/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service; D="$MGR/gopi-probe-$$"
echo "mkdir $D"; mkdir "$D" 2>&1; echo "mkdir exit=$?"; ls -ld "$D" 2>&1
echo "mkdir dir cgroup.procs mode:"; ls -l "$D/cgroup.procs" 2>&1
echo "echo \$\$ > $D/cgroup.procs"; sh -c "echo \$\$ > '$D/cgroup.procs'" 2>&1; echo "write exit=$?"
echo "FRESH read /proc/self/cgroup (the only verdict):"; cat /proc/self/cgroup
echo "rmdir $D"; rmdir "$D" 2>&1; echo "rmdir exit=$?"
sec "cgroupfs: write own pid into the per-uid slice cgroup.procs (transient, must fail)"; sh -c "echo \$\$ > /sys/fs/cgroup/user.slice/user-1000.slice/cgroup.procs" 2>&1; echo "write exit=$?"; cat /proc/self/cgroup
sec "unshare(CLONE_NEWCGROUP)"; unshare -C /bin/true 2>&1; echo "unshare -C exit=$?"
sec "sudo availability"; command -v sudo >/dev/null 2>&1 && echo "sudo present: $(command -v sudo)" || echo "sudo ABSENT"; sudo -n true 2>&1; echo "sudo -n true exit=$?"
sec "setsid only (baseline: session leader, cgroup unchanged)"; setsid sh -c 'echo "setsid child: sid=$$ cgroup=$(cat /proc/self/cgroup)"' 2>&1
sec "nohup/disown are shell builtins with no cgroup effect (no probe possible: they only change SIGHUP/hupone)"
sec "systemd-detect-virt / container?"; systemd-detect-virt; echo "exit=$?"
