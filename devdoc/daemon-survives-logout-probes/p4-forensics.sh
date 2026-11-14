#!/bin/sh
# P4 — forensics on the unlinked user-bus sockets + who could have unlinked them.
# Read-only. No destructive verb appears in this script.
sec() { echo; echo "=== $1 ==="; }
echo "### probe script: p4-forensics.sh"
sec "unlinked-socket ownership recap (fd -> socket inode)"
printf 'pid 2321 fd19 -> %s\n' "$(readlink /proc/2321/fd/19)"
printf 'pid 2321 fd27 -> %s\n' "$(readlink /proc/2321/fd/27)"
printf 'pid 2342 fd3  -> %s\n' "$(readlink /proc/2342/fd/3)"
grep -E '39144|40136' /proc/net/unix
sec "is the path visible in any sampled process's mount view?"
visible=0
for p in /proc/[0-9]*; do
  [ -e "$p/root/run/user/1000/bus" ] && { echo "VISIBLE in $p"; visible=1; }
done
echo "visible_in_any_process=$visible"
sec "entry mtimes: runtime dir vs its children vs the sockets"
stat -c '%n mtime=%y' /run/user/1000 /run/user/1000/dbus-1 /run/user/1000/pulse /run/user/1000/systemd /run/user/1000/systemd/inaccessible 2>&1
sec "user-level tmpfiles rules (can they touch %t = XDG_RUNTIME_DIR?)"
ls -la /usr/lib/user-tmpfiles.d/ /etc/user-tmpfiles.d/ 2>&1
ls -la /home/user/.config/user-tmpfiles.d/ /home/user/.config/tmpfiles.d/ 2>&1
grep -rn -E '%t|/run/user' /usr/lib/user-tmpfiles.d/ 2>/dev/null | head
sec "operator scripts mentioning the runtime dir (search only, no execution)"
grep -rnE 'run/user' /home/user/bin /home/user/.local/bin /home/user/.config/systemd/user 2>/dev/null | head -20
sec "root crontab / cron.d"
ls -la /etc/cron.d/ 2>&1 | head
sec "journal: logind/user-manager messages about the runtime dir or bus this boot"
journalctl -b --no-pager 2>&1 | grep -iE 'runtime director|/run/user/1000|dbus.socket|D-Bus User' | head -20
sec "journal: any message at all in the 2026-09-13 12:12:40 +-2s window"
journalctl --since '2026-09-13 12:12:38' --until '2026-09-13 12:12:44' --no-pager 2>&1
sec "live gopi processes and their cgroups (read-only)"
for p in $(pgrep -x gopi); do echo "pid=$p cgroup=$(cat /proc/$p/cgroup)"; done
sec "gopi scratch GC: which roots has it swept (home= lines, unique)"
grep -oE 'home=[^ ]+' /home/user/.gopi/logs/scratch-gc-watchdog.log | sort -u
sec "gopi scratch GC: any line mentioning /run/user"
grep -n 'run/user' /home/user/.gopi/logs/scratch-gc-watchdog.log | head
sec "TMPDIR / settings tmpdir"
echo "TMPDIR=[${TMPDIR-<unset>}]"
grep -n 'tmpdir' /home/user/.gopi/settings.json 2>/dev/null
echo "shell env from the journal of this agent session:"
