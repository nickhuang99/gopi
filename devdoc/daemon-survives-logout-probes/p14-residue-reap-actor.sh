#!/bin/bash
# p14 — the residue-reap lead: privileged runs from a checkout named
# gopi-residue-reap on this host. Read-only.
echo "### probe script: p14-residue-reap-actor.sh"

echo "=== A. journal: every sudo/privileged line mentioning gopi-residue-reap ==="
journalctl --no-pager -b 2>/dev/null | grep -F 'gopi-residue-reap' | head -20
echo "--- count ---"
journalctl --no-pager -b 2>/dev/null | grep -cF 'gopi-residue-reap'

echo "=== B. privileged commands in the two unlink windows ==="
echo "--- window 1: 2026-09-14 22:30 .. 22:45 ---"
journalctl --no-pager --since '2026-09-14 22:30' --until '2026-09-14 22:45' 2>&1 | grep -E 'sudo|USER=root|rm |unlink' | head -20
echo "--- window 2: 2026-09-15 07:20 .. 07:30 ---"
journalctl --no-pager --since '2026-09-15 07:20' --until '2026-09-15 07:30' 2>&1 | grep -E 'sudo|USER=root|rm |unlink' | head -20

echo "=== C. does that checkout still exist, and what does it do? ==="
ls -ld /home/user/workspace/gopi-residue-reap 2>&1
git -C /home/user/workspace/gopi-residue-reap log --oneline -5 2>&1
git -C /home/user/workspace/gopi-residue-reap branch --show-current 2>&1
echo "--- its own report, if written ---"
ls /home/user/workspace/gopi-residue-reap/devdoc/ 2>/dev/null | grep -iE 'residue|reap|runtime|run-user' | head

echo "=== D. does ANY code in that checkout mention the runtime dir or a memory base? ==="
grep -rn 'XDG_RUNTIME_DIR\|/run/user\|GOPI_SCRATCH_MEMORY_BASE' /home/user/workspace/gopi-residue-reap --include='*.go' --include='*.sh' --include='*.md' 2>/dev/null | head -20

echo "=== E. residue namespaces it collects (does any match bus / systemd/private?) ==="
grep -rn 'suite-tmp-\|gopi-\|shmDir\|MemoryBase' /home/user/workspace/gopi-residue-reap/internal/testutil/legacy_residue.go 2>/dev/null | head -12

echo "=== F. anything root-owned that removes entries with an AGE under /run/user ==="
grep -rn 'run/user' /etc/systemd/system/*.timer /etc/systemd/system/*.service 2>/dev/null | head
ls -la /etc/systemd/system/ 2>/dev/null | grep -iE 'gopi|residue|reap|gc' | head

echo "=== G. the 5 abandoned scopes: are any of them the reaper's sessions? ==="
for s in 15158 15164 15425 17674 18199 18661; do
  echo "--- session-$s ---"
  cat /sys/fs/cgroup/user.slice/user-1000.slice/session-$s.scope/cgroup.procs 2>/dev/null | while read p; do
    printf '  pid=%s %s\n' "$p" "$(tr '\0' ' ' < /proc/$p/cmdline 2>/dev/null | cut -c1-100)"
  done
done
echo "=== end p14 ==="
