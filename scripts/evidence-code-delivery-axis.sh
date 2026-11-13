#!/usr/bin/env bash
# evidence-code-delivery-axis.sh — the MEASURED 3-shape matrix for the epic
# "decouple CODE DELIVERY from the task-ROW switch" (Rule 31: run the surface).
#
# It drives the REAL one-shot pass face (`gopi task sync-pass --once`) three
# times against three DECLARED TOPOLOGIES, and reports which faces ran:
#
#   A  peer declared + row switch OFF   → git/ref pass + adoption sweep RUN,
#                                         no row exchange
#   B  no peer + row switch ON          → NOTHING runs (single-node pure no-op)
#   C  peer declared + row switch ON    → every face runs (the pre-existing
#                                         control: the fixtures are reachable)
#
# Hermetic: GOPI_SETTINGS points at a fixture file, the pass runs inside a
# THROWAWAY git repo, and the declared peer alias cannot resolve (RFC-2606
# `.invalid`), so every git leg fails LOUDLY at ssh and touches no real remote
# — a leg that never dialled cannot name the alias.
#
# It generates NO device load (Rule 27): the scratch root is asserted off the
# `/` partition and holds a handful of tiny commits.
#
# ⚠ IT CANNOT RUN ANY MORE, AND THAT IS RECORDED, NOT HIDDEN. Its subject,
# `gopi task sync-pass --once`, was retired on 2026-11-05 (see
# internal/taskverbs.RemovedVerbs; SyncVerbs is empty). The file is kept as the
# landed evidence of what that pass DID at the revision it was written against —
# the measured matrix below is the artifact, and the command is what it measured.
# Running it today prints the removal sentence at every shape.
set -uo pipefail

# Rule 22: the probe scratch comes from scripts/lib/tmp-ban.sh — the ONE
# reader of the scratch root, asserted off `/` by DEVICE ID and registered
# for trap-time cleanup. The X11 SOCKET directory is the ONE /tmp path this
# repository keeps (internal/shot/x11.go keeps the same one in Go): a unix
# socket there is created by the X server, never written by us, so it is not
# scratch and the off-/ rule does not apply to it. Both come from the helper,
# so neither is spelled here.
. "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/lib/tmp-ban.sh"
X11SOCKDIR="$(gopi_x11_socket_dir)"
PROBEDIR="$(gopi_scratch_mkdir x11-probe)" || exit 1
trap 'gopi_scratch_cleanup' EXIT

HERE="$(cd "$(dirname "$0")/.." && pwd)"
GOPI="$HERE/bin/gopi"
WORKSPACE="${GOPI_EVIDENCE_WORKSPACE:-$HOME/workspace}"
SCRATCH="$WORKSPACE"$PROBEDIR"/code-deliv-axis-$$"
PEER="axis-test-peer.invalid" # MUST NOT resolve: the loud-failure property
mkdir -p "$SCRATCH" || exit 1
trap 'rm -rf "$SCRATCH"' EXIT

# Rule 22: the scratch root is measured off the `/` partition, never assumed.
if [ "$(stat -c %d /)" = "$(stat -c %d "$SCRATCH")" ]; then
    echo "FATAL: scratch root $SCRATCH is on the / partition" >&2
    exit 1
fi
echo "scratch: $SCRATCH (device $(stat -c %d "$SCRATCH") vs / $(stat -c %d /))"

# ---- the throwaway checkout the pass runs inside -------------------------
git init -q -b master "$SCRATCH/repo" || exit 1
git -C "$SCRATCH/repo" config user.email t@t
git -C "$SCRATCH/repo" config user.name t
echo one >"$SCRATCH/repo/a.txt"
git -C "$SCRATCH/repo" add a.txt
git -C "$SCRATCH/repo" commit -qm seed
git -C "$SCRATCH/repo" checkout -qb feat/scratch-deliverable
echo two >>"$SCRATCH/repo/a.txt"
git -C "$SCRATCH/repo" commit -qam deliverable
git -C "$SCRATCH/repo" checkout -q master

# ---- the three settings fixtures ----------------------------------------
printf '{"role":"client","sync_peers":["%s"],"repos":{}}\n' "$PEER" >"$SCRATCH/peer-off.json"
printf '{"task_sync":true,"sync_peers":[],"repos":{}}\n' >"$SCRATCH/nopeer-on.json"
printf '{"task_sync":true,"role":"client","sync_peers":["%s"],"repos":{}}\n' "$PEER" >"$SCRATCH/peer-on.json"

run_shape() { # <label> <settings> <logfile>
    GOPI_SETTINGS="$2" \
        GOPI_TASK_SYNC= \
        GOPI_SYNC_GIT_INTERVAL=1 \
        GOPI_SYNC_INTERVAL=1 \
        GOPI_ADOPTION_INTERVAL=1 \
        GOPI_SYNC_NODE=axis-test-self \
        GOPI_MEMORY_DB="$SCRATCH/ledger-$1.db" \
        "$GOPI" task sync-pass --once >"$3" 2>&1  #retired-verb-ok: HISTORICAL RECORD — this script measured the 3-shape matrix of `task sync-pass --once` on 2026-11-05, and that verb was retired the same day (internal/taskverbs.SyncVerbs is now empty: no manual cross-node verbs remain). It cannot be re-pointed at a live door because the capability it measured no longer exists, and the spelling must stay here verbatim or the recorded matrix would describe a run nobody can account for. Re-running this file prints the removal sentence — that refusal IS the record's correct epilogue; use it as history, not as a check.
    echo "shape $1: exit=$?"
}

cd "$SCRATCH/repo" || exit 1
echo "=== A: peer declared, task-row switch OFF ==="
run_shape A "$SCRATCH/peer-off.json" "$SCRATCH/A.log"
cat "$SCRATCH/A.log"
echo "--- A: faces observed ---"
echo "peer dialled (delivery reached git): $(grep -c "$PEER" "$SCRATCH/A.log") line(s)"
echo "row exchange (delta legs):           $(grep -c '\[serve\] sync' "$SCRATCH/A.log") line(s)"

echo "=== B: no peer, task-row switch ON ==="
run_shape B "$SCRATCH/nopeer-on.json" "$SCRATCH/B.log"
cat "$SCRATCH/B.log"
echo "--- B: faces observed ---"
echo "peer dialled:      $(grep -c "$PEER" "$SCRATCH/B.log") line(s)"
echo "row exchange:      $(grep -c '\[serve\] sync' "$SCRATCH/B.log") line(s)"

echo "=== C: peer declared, task-row switch ON (control) ==="
run_shape C "$SCRATCH/peer-on.json" "$SCRATCH/C.log"
cat "$SCRATCH/C.log"
echo "--- C: faces observed ---"
echo "peer dialled:      $(grep -c "$PEER" "$SCRATCH/C.log") line(s)"
echo "row exchange:      $(grep -c '\[serve\] sync' "$SCRATCH/C.log") line(s)"

echo "=== matrix (measured, 3 shapes) ==="
printf '%-6s %-30s %-22s %-26s %s\n' shape topology "branches delivered" "branches adopted" "rows exchanged"
for L in A B C; do
    case $L in
    A) topo="peer declared, row switch OFF" ;;
    B) topo="no peer, row switch ON" ;;
    C) topo="peer declared, row switch ON" ;;
    esac
    hit=$(grep -c "\[serve\] \[git-" "$SCRATCH/$L.log")
    ad=$(grep -c "\[serve\] \[adopt\]" "$SCRATCH/$L.log")
    rx=$(grep -c "\[serve\] sync" "$SCRATCH/$L.log")
    printf '%-6s %-30s %-22s %-26s %s\n' "$L" "$topo" "$hit line(s)" "$ad line(s)" "$rx line(s)"
done
