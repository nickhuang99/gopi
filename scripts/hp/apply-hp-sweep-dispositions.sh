#!/bin/bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# apply-hp-sweep-dispositions.sh — the disposition half of the HP sweep
# (devdoc/hp-inline-launchers-20260912.md §sweep). Run ON HP:
#
#     ssh hp 'bash -s' < scripts/hp/apply-hp-sweep-dispositions.sh
#
# SAFETY: it touches NO running process and NO worker (no pkill, no pgrep -f
# kill, no PID it did not create). Three narrow actions only:
#
#   A. de-execute the PRE-FIX inline starters that sit in ~/bin (the two
#      *.bak-* copies of the old unsupervised scripts). They are the deleted
#      defect kept as evidence — a loose EXECUTABLE copy in a PATH directory
#      is still a launcher-shaped path, so the exec bit goes, the bytes stay.
#   B. delete the probe stubs this epic's own probe wrote (~/.gopi-ship/probe)
#      — regenerable test scaffolding, never a worker path.
#   C. print the sha of the staging copy vs the installed launcher, so a
#      "which bytes are installed?" question is answered by evidence.
set -u

echo "=== A: de-execute the pre-fix inline starter copies ==="
for f in /home/nick/bin/*.bak-* /home/nick/.gopi/hp-night-legacy-audit.sh.bak-*; do
  [ -e "$f" ] || continue
  chmod a-x "$f"
  echo "de-executed: $f"
done
echo "=== A2: de-execute the ship-staging launcher copy (the installed one is authoritative) ==="
if [ -e /home/nick/.gopi-ship/gopi-worker-up.sh ]; then
  chmod a-x /home/nick/.gopi-ship/gopi-worker-up.sh
  echo "de-executed (bytes kept as ship evidence): /home/nick/.gopi-ship/gopi-worker-up.sh"
fi
ls -l /home/nick/bin/ | grep -E 'bak-|worker-'
ls -l /home/nick/.gopi/hp-night-legacy-audit.sh* /home/nick/.gopi-ship/gopi-worker-up.sh

echo "=== B: remove the epic's own regenerable probe stubs ==="
rm -rf /home/nick/.gopi-ship/probe
ls -la /home/nick/.gopi-ship/

echo "=== C: staging copy vs installed launcher (same bytes = a real ship) ==="
sha256sum /home/nick/.gopi-ship/gopi-worker-up.sh /home/nick/.local/bin/gopi-worker-up.sh

echo "=== D: final state — no live gopi worker, launcher present ==="
pgrep -ax gopi || echo "(no live gopi processes)"
ls -l /home/nick/bin/gopi-worker-up.sh
