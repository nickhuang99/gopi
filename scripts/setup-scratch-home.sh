#!/usr/bin/env bash
# setup-scratch-home.sh — RETIRED. The scratch home stays on the block device.
#
# WHY THIS IS NOW A NO-OP (operator ruling, 2026-09-18):
#   "just for a few ms performance? no, no no! don't use tmpfs. change settings
#    to workspace. now"
#
# The memory-backed route this script used to install (a tmpfs mount, or a
# symlink to /dev/shm on an unprivileged host) was added on 2026-09-11 to buy
# wall-clock time: a block-backed scratch root makes every SQLite scratch ledger
# pay a block-device fsync at each WAL close, measured as 601s for
# internal/tools on ext4 /home versus 237s on tmpfs — a 2.5x tax that decided a
# go-test timeout (a FAIL at 601s against the 10-minute default).
#
# The operator weighed that against the cost of the memory backing and rejected
# it. The scratch home is now a REAL DIRECTORY on the block device:
#
#   <home>/workspace/gopi-test-scratch     (device /dev/sda4 — /home: 766G)
#
# The hard constraint every suite asserts is still satisfied:
# internal/testutil.AssertSuiteScratchRootSafe requires the scratch root to be
# off the `/` DEVICE (147G, the 2026-09-11 exhaustion site), and /home is a
# different device (sda4 vs sda3). So nothing else had to change.
#
# This script is KEPT, not deleted, because it is the record of the measurement
# above — a future operator may want to re-measure the fsync tax. It CHANGES
# NOTHING on any invocation: no mount, no symlink, no migration. It reports the
# current state and exits 0, and it shouts if it finds the old symlink back.
#
# Usage: scripts/setup-scratch-home.sh

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"

HOME_DIR="$(gopi_scratch_home)"

echo "setup-scratch-home: RETIRED — the scratch home stays on the block device"
echo "  scratch home : $HOME_DIR"

if [ -L "$HOME_DIR" ]; then
    echo "  state        : a SYMLINK to $(readlink "$HOME_DIR")" >&2
    echo "  action       : NONE taken, but this is NOT the intended state." >&2
    echo "                 The ruling is a real directory on /home. Restore with:" >&2
    echo "                   rm \"$HOME_DIR\" && mkdir -p \"$HOME_DIR\"" >&2
elif [ -d "$HOME_DIR" ]; then
    dev="$(stat -c %d "$HOME_DIR")"
    rootdev="$(stat -c %d /)"
    echo "  device       : $dev (/ is $rootdev)"
    if [ "$dev" = "$rootdev" ]; then
        echo "  WARNING      : this IS the / device — every suite asserts otherwise" >&2
    else
        echo "  ok           : off the / device, as AssertSuiteScratchRootSafe requires"
    fi
else
    echo "  state        : does not exist — create it with: mkdir -p \"$HOME_DIR\""
fi

echo "setup-scratch-home: nothing was changed"
