#!/bin/bash
# p7 — pin the root cause with inode/ctime + logged evidence. Read-only: no writes,
# no creates, no removals anywhere. Every command is a measurement.
echo "### probe script: p7-rootcause-pin.sh"

echo "=== 1. inode/ctime/links of the runtime dir and systemd/ (ctime moves on any unlink) ==="
stat -c 'ino=%i links=%h ctime=%z mtime=%y %n' /run/user/1000 /run/user/1000/systemd /run/user/1000/systemd/inaccessible 2>&1

echo "=== 2. the two sockets: what does the kernel have, what does the VFS have ==="
echo "--- /proc/net/unix (kernel's bound-path list; a bound AF_UNIX path is printed even after unlink) ---"
grep -E ' 40136 | 39144 ' /proc/net/unix 2>&1
echo "--- VFS: does the path exist? (stat follows the symlink-free path) ---"
stat /run/user/1000/bus 2>&1
stat /run/user/1000/systemd/private 2>&1
echo "--- socket(7) level check: file type + inode of any live socket node ---"
ls -la /run/user/1000/bus /run/user/1000/systemd/private 2>&1

echo "=== 3. the whole systemd/ subtree the manager keeps, as the MANAGER sees it ==="
find /proc/2321/root/run/user/1000/systemd -maxdepth 3 -printf '%y %i %p\n' 2>&1

echo "=== 4. journal: did the user manager ever recreate its private socket this boot? ==="
journalctl --no-pager -b 2>/dev/null | grep -E 'systemd\[2321\]' | grep -iE 'private|bus|socket' | tail -20
echo "--- boot-wide: any 'D-Bus User Message Bus' line for uid 1000 ---"
journalctl --no-pager -b 2>/dev/null | grep -E 'D-Bus User Message Bus' | tail -10

echo "=== 5. unit state of the two units that CREATE the runtime dir ==="
systemctl show user-runtime-dir@1000.service -p Id -p ActiveState -p SubState -p ExecMainStartTimestamp -p ExecMainPID -p Result 2>&1
echo "--- unit file (verbatim) ---"
cat /lib/systemd/system/user-runtime-dir@.service 2>&1
echo "--- drop-in on user@.service (the one status names) ---"
cat /usr/lib/systemd/system/user@.service.d/timeout.conf 2>&1

echo "=== 6. the operator's GC script: the runtime-dir site in context ==="
sed -n '70,130p' /home/user/.local/bin/gopi-home-gc.sh 2>&1

echo "=== 7. is the runtime dir being used as a scratch/test root (who writes there) ==="
ls -1 /run/user/1000 | grep -c . 2>&1
ls -1d /run/user/1000/mv-* /run/user/1000/gopi-* /run/user/1000/daemoncwd-* 2>&1 | head -10
echo "--- processes with a cwd under /run/user/1000 ---"
for p in /proc/[0-9]*; do c=$(readlink $p/cwd 2>/dev/null); case "$c" in /run/user/1000*) echo "pid=${p#/proc/} cwd=$c";; esac; done | head -10

echo "=== 8. XDG_RUNTIME_DIR consumers in the gopi tree (read-only grep, main repo) ==="
grep -rn 'XDG_RUNTIME_DIR' /home/user/work/GoPi --include='*.go' 2>/dev/null | head -20
grep -rn 'GOPI_TEST_TMPDIR' /home/user/work/GoPi --include='*.go' 2>/dev/null | head -10

echo "=== 9. sanity: the SECOND independent path into a working session bus (X11 autolaunch) ==="
echo "DISPLAY=[${DISPLAY-<unset>}] XAUTHORITY=[${XAUTHORITY-<unset>}]"
grep -rn '^Exec\|^Name' /usr/share/dbus-1/services/org.freedesktop.DBus 2>/dev/null | head
ls -la /run/user/1000/dbus-1/ 2>&1

echo "=== end p7 ==="
