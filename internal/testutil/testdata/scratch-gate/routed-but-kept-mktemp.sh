#!/bin/bash
# The 2026-10-07 MARGINAL PROBE (task 01a1137f): the shape the old predicate
# passed. It ROUTES to the shared helper with a real `. ` statement — and keeps
# its own `mktemp -d`, so its scratch still lands wherever $TMPDIR points.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/../../../scripts/lib/tmp-ban.sh" || exit 1
SCRATCH_PARENT="${TMPDIR:-$HOME/.cache}"
SB=$(mktemp -d -p "$SCRATCH_PARENT" marginal-probe.XXXXXX) || exit 1
echo "$SB"
