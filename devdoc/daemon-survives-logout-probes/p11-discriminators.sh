#!/bin/bash
# p11 — discriminators: is the missing-bus state per-user (the operator-only) or host-wide?
# Read-only.
echo "### probe script: p11-discriminators.sh"

echo "=== A. a SIBLING user's runtime dir (uid 117, lightdm) — the control group ==="
ls -la /run/user/117/ 2>&1
echo "--- its systemd/ subdir ---"
ls -la /run/user/117/systemd/ 2>&1
echo "--- does the control user's user manager exist? ---"
systemctl is-active user@117.service 2>&1
echo "--- its socket list, if any ---"
grep -E '/run/user/117' /proc/net/unix 2>&1

echo "=== B. exact journal window 07:26:10 .. 07:26:30 today (all units) ==="
journalctl --no-pager --since '2026-09-15 07:26:10' --until '2026-09-15 07:26:30' 2>&1 | tail -20

echo "=== C. exact journal window 2026-09-14 22:39:05 .. 22:39:15 (the dir unlink) ==="
journalctl --no-pager --since '2026-09-14 22:39:05' --until '2026-09-14 22:39:15' 2>&1 | grep -vE 'ScreenSaver|global-scheduler' | tail -20

echo "=== D. any process whose cwd/fd lives directly under /run/user/1000 (potential unlinker) ==="
for p in /proc/[0-9]*; do
  pid=${p#/proc/}
  c=$(readlink $p/cwd 2>/dev/null)
  case "$c" in /run/user/1000) echo "pid=$pid CWD=/run/user/1000 cmd=$(tr '\0' ' ' < $p/cmdline 2>/dev/null | cut -c1-90)";; esac
done | head -10

echo "=== E. the destructive primitives that COULD unlink a socket path ==="
echo "--- recent shell history mentioning the runtime dir ---"
grep -nE 'run/user|XDG_RUNTIME_DIR' /home/user/.bash_history 2>/dev/null | tail -10
echo "--- gopi repo: any code that removes entries it did not create (unnamed/unknown) ---"
grep -rn 'os.Remove(\|os.RemoveAll(' /home/user/work/GoPi/internal/testutil/scratchroot.go 2>/dev/null

echo "=== F. surviving evidence of what a sweep matches (name patterns only) ==="
ls -1 /run/user/1000 | grep -vE '^(mv-|gopi-|daemoncwd-|snap\.|at-spi|pulse|dbus-1|dconf|ccache-tmp|doc|gvfs|gvfsd|systemd)' 2>&1

echo "=== G. private/notify socket presence in OTHER users' managers (container/system managers) ==="
for d in /run/user/*/systemd; do printf '%s : ' "$d"; ls -1 "$d" 2>/dev/null | tr '\n' ' '; echo; done

echo "=== H. is there a systemd unit/timer/cron that touches $XDG_RUNTIME_DIR? ==="
grep -rln 'run/user\|XDG_RUNTIME_DIR' /etc/cron.d/ /etc/cron.hourly/ /etc/cron.daily/ /etc/systemd/system/ /home/user/.config/systemd/user/ 2>/dev/null | head -10
echo "=== end p11 ==="
