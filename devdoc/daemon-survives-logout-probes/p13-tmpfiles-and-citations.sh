#!/bin/bash
# p13 — evidence hygiene: archive the tmpfiles/residue check and re-verify the
# verbatim quotes used in the devdoc against the retrieved upstream pages.
echo "### probe script: p13-tmpfiles-and-citations.sh"

echo "=== A. /etc/tmpfiles.d inventory + the residue conf referenced in the journal ==="
cat /etc/tmpfiles.d/gopi-residue.conf 2>&1
ls -la /etc/tmpfiles.d/ 2>&1
echo "--- journal lines that referenced it (privileged, 2026-09-13) ---"
journalctl --no-pager -b 2>/dev/null | grep -F 'gopi-residue' | tail -5
echo "--- does the system tmpfiles-clean sweep the runtime dir? (rules + last runs) ---"
grep -rnE '^[a-zA-Z]' /usr/lib/tmpfiles.d/systemd.conf /usr/lib/tmpfiles.d/gvfsd-fuse-tmpfiles.conf 2>&1 | grep -E '/run/user|^/usr.*:[0-9]+:[a-zA-Z]' | head
journalctl --no-pager -b 2>/dev/null | grep -E 'systemd-tmpfiles-clean' | tail -3

echo "=== B. the doc's key host quotes, re-read verbatim (so the doc and host agree) ==="
echo "--- linger file + logind effective config ---"
ls -la /var/lib/systemd/linger/ 2>&1
grep -vE '^\s*#|^\s*$' /etc/systemd/logind.conf 2>&1
echo "--- user@.service: the delegation + dependency lines ---"
grep -E '^(After|Requires|Delegate|KillMode|Slice|PAMName|Type)=' /lib/systemd/system/user@.service 2>&1
echo "--- user-runtime-dir@.service: the create/remove contract ---"
grep -E '^(Description|ExecStart|ExecStop|RemainAfterExit|StopWhenUnneeded)=' /lib/systemd/system/user-runtime-dir@.service 2>&1
echo "--- the user dbus.socket unit (the unit that would own %t/bus) ---"
cat /usr/lib/systemd/user/dbus.socket 2>&1

echo "=== C. abandoned-scope arithmetic (recounted, so the doc's total is arithmetically checkable) ==="
tot=0
for s in session-15158 session-15164 session-15425 session-17674 session-18199 session-18661; do
  n=$(wc -l < /sys/fs/cgroup/user.slice/user-1000.slice/$s.scope/cgroup.procs 2>/dev/null || echo 0)
  printf '%s=%s ' "$s" "$n"; tot=$((tot+n))
done
echo; echo "TOTAL_ABANDONED_SCOPE_PROCS=$tot"
echo "--- and the sessions logind still knows about ---"
loginctl list-sessions --no-legend 2>&1 | awk '{printf "%s ", $1}'; echo

echo "=== D. citation reachability (each URL the devdoc cites, one HEAD request) ==="
for u in \
  https://man.archlinux.org/man/loginctl.1.en \
  https://man.archlinux.org/man/user@.service.5.en \
  https://man.archlinux.org/man/logind.conf.5.en \
  https://man.archlinux.org/man/systemd-run.1.en \
  https://man.archlinux.org/man/systemd.scope.5.en \
  https://man.archlinux.org/man/systemd.kill.5.en \
  https://man.archlinux.org/man/systemd.special.7.en \
  https://systemd.io/CONTROL_GROUP_INTERFACE/ \
  https://docs.kernel.org/admin-guide/cgroup-v2.html \
  https://specifications.freedesktop.org/basedir-spec/latest/ \
  https://dbus.freedesktop.org/doc/dbus-daemon.1.html \
  https://wiki.archlinux.org/title/Systemd/User ; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -L --max-time 20 "$u" 2>/dev/null)
  printf '%s %s\n' "$code" "$u"
done
echo "=== end p13 ==="
