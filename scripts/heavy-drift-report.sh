#!/usr/bin/env bash
# heavy-drift-report.sh — name the tests that cross the classification floor
# WITHOUT a heavy-classification row. Called by scripts/test-budget.sh on every
# full smoke run (and runnable by hand over any preserved stream).
#
# WHY (the gap this closes)
# -------------------------
# testdata/heavy-tests.tsv is a SNAPSHOT: derived once from one measured run
# (2026-09-16), and the inventory gate keeps it truthful about the CALL SITES it
# lists. Neither half can notice a test that becomes long LATER — and such a
# test is exactly the one that silently eats the smoke budget: it is not in the
# inventory (so no gate asks about it) and not in .heavy-skipped.tsv (so no
# reviewer sees it either). This report names it, using the run's own durations,
# and names the exact command that re-derives the inventory.
#
# IT REPORTS, IT DOES NOT FAIL. A threshold alert against a shared, load-varying
# host is a LEAD for a human (at load 42-63 this suite's wall time triples), not
# a red build — a probe that fires on host noise is one a reader learns to
# ignore, which is worse than no probe. The mechanical half stays where it is
# deterministic: the inventory gate fails on drift between the inventory and the
# call sites.
#
# USAGE
#   scripts/heavy-drift-report.sh [STREAM]      # default: scratch queue last.json
#
# Exit codes: 0 = report printed, 2 = usage/setup error (no stream to read).

set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$root/scripts/lib/tmp-ban.sh"
# shellcheck source=lib/heavy-threshold.sh
. "$root/scripts/lib/heavy-threshold.sh"

budget_dir="$(gopi_scratch_queue)/test-budget"
stream="${1:-$budget_dir/last.json}"
if [ ! -f "$stream" ]; then
    echo "heavy-drift-report: no stream at $stream — run \`scripts/test-budget.sh ./...\`" >&2
    echo "heavy-drift-report: (or pass a preserved run-<epoch>.json path) first." >&2
    exit 2
fi
command -v python3 >/dev/null 2>&1 || exit 0
[ -f "$root/$heavy_inventory_rel" ] || exit 0

python3 - "$stream" "$root/$heavy_inventory_rel" "$heavy_slow_threshold_s" "$heavy_gate_packages" "$heavy_derive_cmd" <<'PY'
import json, sys

stream, inventory, floor, gate_pkgs, derive_cmd = (
    sys.argv[1], sys.argv[2], float(sys.argv[3]), sys.argv[4].split(), sys.argv[5])

# The inventory lists TOP-LEVEL tests only (heavytest.Require is a test-body
# statement, so a subtest cannot carry a row). Keyed by (package dir, test) —
# the same shape the inventory gate matches.
classified = set()
with open(inventory, errors="replace") as fh:
    for line in fh:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) >= 3:
            classified.add((parts[2].rsplit("/", 1)[0], parts[0]))

rows = {}    # (pkg, top-level test) -> elapsed
subtests = {}  # (pkg, test) -> summed elapsed, folded into the parent
for line in open(stream, errors="replace"):
    line = line.strip()
    if not line.startswith("{"):
        continue
    try:
        ev = json.loads(line)
    except ValueError:
        continue
    action, pkg, test = ev.get("Action"), ev.get("Package", "?"), ev.get("Test")
    if not test:
        continue
    if action == "run":
        parent = test.split("/", 1)[0]
        subtests[(pkg, parent)] = subtests.get((pkg, parent), 0.0) + float(ev.get("Elapsed") or 0.0)
    elif action in ("pass", "fail"):
        rows[(pkg, test)] = float(ev.get("Elapsed") or 0.0)
    elif action == "skip":
        rows.pop((pkg, test), None)
for key, sec in subtests.items():
    if key in rows:
        rows[key] = max(rows[key], sec)

slow = {k: v for k, v in rows.items() if "/" not in k[1] and v >= floor}
# Gate packages can NEVER be classified heavy (a merge must not land with its
# own gates skipped), so naming them would be unactionable noise.
slow = {k: v for k, v in slow.items() if not any(g in k[0] for g in gate_pkgs)}
unclassified = {k: v for k, v in slow.items() if (k[0].split("/gopi/", 1)[-1], k[1]) not in classified}

print("heavy classification floor: %.0fs — %d test(s) at or above it, %d without a %s row"
      % (floor, len(slow), len(unclassified), inventory.split("/")[-1]))
for (pkg, test), sec in sorted(unclassified.items(), key=lambda kv: kv[1], reverse=True):
    print("  UNCLASSIFIED %7.2fs  %-18s %s" % (sec, pkg.rsplit("/", 1)[-1], test))
if unclassified:
    print("  -> either classify it (heavytest.Require(t, Criterion*) + an inventory row)")
    print("     or accept it, and re-derive the whole list from a real run:")
    print("     %s" % derive_cmd)
elif slow:
    print("  every slow test is accounted for by the inventory")
PY
