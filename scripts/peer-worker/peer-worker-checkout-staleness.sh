#!/bin/bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# peer-worker-checkout-staleness.sh — report HP's checkout/binary staleness as evidence
# for devdoc/hp-inline-launchers-20260912.md §staleness. Read-only.
set -u
echo "=== checkouts on HP (HEAD + date) ==="
for d in /home/user/work/GoPi /home/user/work/gopi-wprov; do
  [ -d "$d/.git" ] || continue
  printf '%-40s %s  %s  %s\n' "$d" \
    "$(cd "$d" && git rev-parse --short HEAD 2>/dev/null)" \
    "$(cd "$d" && git log -1 --format=%ad --date=short 2>/dev/null)" \
    "$(cd "$d" && git log -1 --format=%s 2>/dev/null | cut -c1-60)"
done
echo
echo "=== the resolved worker binary ==="
readlink -f /usr/local/bin/gopi
ls -l /home/user/work/GoPi/bin/gopi 2>&1
echo
echo "=== do the guard / the log-root flag exist in that binary? ==="
# NOTE (epic child-stream-task-output, 2026-09-17): --worker-log-root is no
# longer a worker OPTION, but the string is still a valid staleness probe — a
# current binary carries it in the refusal that names the retired flag and its
# replacement, so the count stays > 0 for a fresh build and 0 for a build older
# than the guard.
for s in "refusing to start a worker" "worker-log-root" "GOPI_AGENT_SESSION" "GOPI_WORKER_MAX_ROUNDS"; do
  printf '  %-30s %s\n' "$s" "$(strings /home/user/work/GoPi/bin/gopi 2>/dev/null | grep -c "$s")"
done
echo
echo "=== is the launcher in the checkout (working tree / git)? ==="
ls -l /home/user/work/GoPi/scripts/gopi-worker-up.sh 2>&1
(cd /home/user/work/GoPi && git cat-file -e master:scripts/gopi-worker-up.sh 2>&1 && echo "  in the git object store" || echo "  NOT in the git object store")
echo
echo "=== last fetch from peerbox ==="
ls -l /home/user/work/GoPi/.git/FETCH_HEAD 2>&1
