#!/bin/bash
# p6 — who deleted the socket paths? (read-only; nothing created or removed)
echo "### probe script: p6-deletion-forensics.sh"

echo "=== A. mtime/ino sampling of /run/user/1000 + /run/user/1000/systemd (4 samples, 2s apart) ==="
for i in 1 2 3 4; do
  stat -c '%y ino=%i %n' /run/user/1000 /run/user/1000/systemd /run/user/1000/systemd/inaccessible 2>&1 | tr '\n' '|'
  echo
  sleep 2
done

echo "=== B. what my own earlier probe scripts did to those paths (p2 script verbatim) ==="
cat /dev/shm/gopi-daemon-research/p2-userbus.sh

echo "=== C. harness scripts left in the runtime dir (verbatim, first 40 lines each) ==="
for f in /run/user/1000/mv-run.sh /run/user/1000/mv-run2.sh /run/user/1000/mv-run3.sh /run/user/1000/mv-run4.sh; do
  echo "--- $f ---"
  head -40 "$f" 2>&1
done

echo "=== D. any script/unit that removes entries under the runtime dir ==="
grep -rn 'XDG_RUNTIME_DIR' /home/user/work/GoPi/internal /home/user/work/GoPi/cmd /home/user/work/GoPi/testsuite 2>/dev/null | head -20
echo "--- scratch/tmp-root code that could point at the runtime dir ---"
grep -rn 'XDG_RUNTIME_DIR\|/run/user' /home/user/work/GoPi/internal/scratch /home/user/work/GoPi/internal/testutil 2>/dev/null | head -20

echo "=== E. the manager's own log around the two candidate moments ==="
echo "--- Sep 14 22:38:50 .. 22:39:20 (runtime-dir mtime) ---"
journalctl --no-pager --since '2026-09-14 22:38:50' --until '2026-09-14 22:39:20' 2>&1 | grep -vE 'global-scheduler|CONFIG-CHECK' | tail -25
echo "--- today 07:25:30 .. 07:27:00 (systemd/ mtime) ---"
journalctl --no-pager --since '2026-09-15 07:25:30' --until '2026-09-15 07:27:00' 2>&1 | tail -25

echo "=== F. systemd's own notion of the private socket / bus (system bus, read-only) ==="
systemctl status user@1000.service --no-pager 2>&1 | head -12

echo "=== G. does anything still hold the unlinked sockets (fds + inode) ==="
ls -l /proc/2321/fd 2>/dev/null | grep -E 'socket:\[(40136|39144)\]'
ls -l /proc/2342/fd 2>/dev/null | grep -E 'socket:\[(40136|39144)\]'

echo "=== H. uptime/manager start vs the unlink moment ==="
echo "boot:            $(uptime -s)"
echo "manager 2321 start: $(stat -c %y /proc/2321)"
echo "runtime dir mtime:  $(stat -c %y /run/user/1000)"
echo "=== end p6 ==="
