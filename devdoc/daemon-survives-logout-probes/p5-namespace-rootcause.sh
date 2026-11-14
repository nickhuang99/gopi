#!/bin/bash
# p5 — root-cause forensics for the ABSENT user bus paths. Read-only.
echo "### probe script: p5-namespace-rootcause.sh"

echo "=== manager pid 2321: root link + mount namespace ==="
readlink /proc/2321/root
readlink /proc/2321/ns/mnt
readlink /proc/self/ns/mnt
readlink /proc/2342/ns/mnt

echo "=== stat: /run/user/1000 in OUR ns vs via the MANAGER's root ==="
stat -c '%d:%i %n' /run/user/1000 2>&1
stat -L -c '%d:%i %n' /proc/2321/root/run/user/1000 2>&1

echo "=== the MANAGER's own view of /run/user/1000 ==="
ls -la /proc/2321/root/run/user/1000/ 2>&1

echo "=== the MANAGER's own view of /run/user/1000/systemd ==="
ls -la /proc/2321/root/run/user/1000/systemd/ 2>&1

echo "=== dbus-daemon 2342's own view ==="
readlink /proc/2342/root
ls -la /proc/2342/root/run/user/1000/ 2>&1

echo "=== OUR view (comparison) ==="
ls -la /run/user/1000/ 2>&1

echo "=== systemd's unit files for the runtime dir + user manager ==="
for f in /usr/lib/systemd/system/user-runtime-dir@.service /usr/lib/systemd/system/user@.service; do
  echo "--- $f ---"
  cat "$f" 2>&1
done

echo "=== drop-ins ==="
ls -la /etc/systemd/system/user@.service.d/ /usr/lib/systemd/system/user@.service.d/ 2>&1

echo "=== dbus user units present on this host ==="
ls -la /usr/lib/systemd/user/dbus.socket /usr/lib/systemd/user/dbus.service /usr/lib/systemd/user/dbus-broker.socket 2>&1

echo "=== systemctl status user-runtime-dir@1000.service (system bus) ==="
systemctl status user-runtime-dir@1000.service --no-pager 2>&1 | head -25
echo "-----"
echo "=== systemctl status user@1000.service ==="
systemctl status user@1000.service --no-pager 2>&1 | head -20

echo "=== logind record ==="
loginctl show-user the operator -p RuntimePath -p Service -p Slice -p Linger -p State -p Timestamp 2>&1

echo "=== journal (this boot): user-runtime-dir@1000 / user@1000 ==="
journalctl --no-pager -b 2>/dev/null | grep -E 'user-runtime-dir@1000|user@1000\.service|User Runtime Directory /run/user/1000' | tail -30

echo "=== journal: the Sep 14 22:39:10 window (the /run/user/1000 mtime) ==="
journalctl --no-pager --since '2026-09-14 22:38:30' --until '2026-09-14 22:40:00' 2>&1 | tail -40

echo "=== boot time vs now ==="
uptime -s

echo "=== tmpfs mount entries for the runtime dir ==="
grep -E ' /run/user/1000 ' /proc/self/mountinfo 2>&1
grep -E '/run/user' /proc/2321/mountinfo 2>&1 | head

echo "=== notify socket / systemd dir present? ==="
ls -la /run/user/1000/systemd/ 2>&1

echo "=== XDG_RUNTIME_DIR / DBUS_SESSION_BUS_ADDRESS as the MANAGER sees them ==="
tr '\0' '\n' < /proc/2321/environ 2>&1 | grep -E '^(XDG_RUNTIME_DIR|DBUS_SESSION_BUS_ADDRESS)='

echo "=== /proc/net/unix entries for the two unlinked sockets ==="
grep -E '40136|39144' /proc/net/unix 2>&1

echo "=== open fds of manager + dbus-daemon pointing at those inodes ==="
ls -l /proc/2321/fd 2>/dev/null | grep -E 'socket:\[(40136|39144)\]'
ls -l /proc/2342/fd 2>/dev/null | grep -E 'socket:\[(40136|39144)\]'

echo "=== sanity: does ANY process's filesystem view carry the path? (bounded sample) ==="
found=0
for d in /proc/[0-9]*; do
  pid=${d#/proc/}
  if [ -e "$d/root/run/user/1000/bus" ]; then echo "FOUND in pid $pid via /proc/$pid/root"; found=1; fi
done
echo "visible_in_any_process=$found"

echo "=== end p5 ==="
