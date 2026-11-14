#!/bin/bash
# p8 — is a gopi scratch/tmp GC capable of unlinking the socket paths? Read-only.
echo "### probe script: p8-gc-candidate.sh"

echo "=== 1. the operator GC script, in full ==="
wc -l /home/user/.local/bin/gopi-home-gc.sh
sed -n '85,115p' /home/user/.local/bin/gopi-home-gc.sh
echo "--- removal primitives used by that script ---"
grep -nE '\brm\b|find .*-delete|unlink|shred' /home/user/.local/bin/gopi-home-gc.sh | head -30

echo "=== 2. gopi scratch GC: what does it delete, and under which root? ==="
grep -rn 'func .*GC\|os.RemoveAll\|os.Remove(' /home/user/work/GoPi/internal/scratch/*.go 2>/dev/null | head -30
echo "--- the scratch root resolution (env names + defaults) ---"
grep -rn 'GOPI_TEST_SCRATCH_HOME\|GOPI_TEST_TMPDIR\|TMPDIR\|os.TempDir()' /home/user/work/GoPi/internal/scratch/*.go /home/user/work/GoPi/internal/testutil/*.go 2>/dev/null | head -30

echo "=== 3. does any harness set the tmp/scratch root to the runtime dir? ==="
grep -rn 'XDG_RUNTIME_DIR' /home/user/work/GoPi --include='*.go' --include='*.sh' 2>/dev/null | head -20
echo "--- scripts under home referencing the runtime dir as a root ---"
grep -rn 'GOPI_TEST_TMPDIR=\|GOPI_TEST_SCRATCH_HOME=' /home/user/.local/bin/*.sh /home/user/work/GoPi/scripts/*.sh 2>/dev/null | head -20

echo "=== 4. residue at the runtime dir ROOT: was the dir itself used as a scratch root? ==="
ls -la /run/user/1000/daemoncwd-holder-fixtures 2>&1 | head
ls -la /run/user/1000/mv-c 2>&1 | head -5
ls -la /run/user/1000/gopi-scratch-verify 2>&1 | head -5
echo "--- mtimes of the residue (brackets the last sweep window) ---"
stat -c '%y %n' /run/user/1000/mv-c /run/user/1000/mv-p /run/user/1000/ccache-tmp /run/user/1000/gopi-scratch-verify 2>&1

echo "=== 5. is a GC timer/daemon scheduled on this host? ==="
systemctl --user list-units --all --no-pager 2>&1 | grep -iE 'gc|scratch|sweep|gopi' | head -10
systemctl list-timers --all --no-pager 2>&1 | head -12
echo "--- crontab (user) ---"
crontab -l 2>&1 | head -10

echo "=== 6. scratch GC docs: what it claims to remove ==="
ls /home/user/work/GoPi/devdoc/ | grep -iE 'scratch|gc' | head -10
grep -rn 'socket' /home/user/work/GoPi/internal/scratch/*.go 2>/dev/null | head -10

echo "=== 7. the two sockets' holders, once more (unchanged?) ==="
ls -l /proc/2321/fd 2>/dev/null | grep -cE 'socket:\[(40136|39144)\]'
ls -l /proc/2342/fd 2>/dev/null | grep -E 'socket:\[40136\]'
echo "=== end p8 ==="
