#!/usr/bin/env bash
# heavy-derive.sh — RE-DERIVE the heavy-test classification candidates from a
# real run's own `go test -json` stream. This is the documented recipe
# (devdoc/heavy-tests-rederivation.md) as a runnable script, so the inventory's
# provenance is reproducible instead of trusted.
#
# WHY
# ---
# testdata/heavy-tests.tsv was derived ONCE (2026-09-16) from one run's
# durations. The inventory gate keeps the file truthful about the call sites it
# lists; it cannot know about a test that has since become long. This script is
# the other direction: durations -> candidate rows, from a stream you just
# produced. `scripts/test-budget.sh` reports the same drift on every run; this
# prints the whole candidate set, ready to paste into the inventory.
#
# WHAT IT EXCLUDES (the classification rules, from the devdoc)
#   - gate packages (internal/standards, internal/stylegate): a merge must never
#     land with its own gates skipped — the inventory gate fails if anyone tries;
#   - subtests: heavytest.Require is a test-body statement, so only top-level
#     tests can be classified (a subtest's elapsed is folded into its parent's
#     for the threshold comparison, but never printed as a row);
#   - tests already carrying a heavytest.Require call site (already classified).
#
# USAGE
#   scripts/test-budget.sh ./...            # produce a stream (any full smoke run)
#   scripts/heavy-derive.sh                 # latest stream under the scratch queue
#   scripts/heavy-derive.sh path/to/run-*.json
#
# Exit codes: 0 = candidates printed (possibly none), 2 = usage/setup error
# (no stream, unreadable inventory, stream too small to be a real run).

set -uo pipefail

# The floor + inventory path: ONE definition, shared with test-budget.sh.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$root/scripts/lib/tmp-ban.sh"
# shellcheck source=lib/heavy-threshold.sh
. "$root/scripts/lib/heavy-threshold.sh"

budget_dir="$(gopi_scratch_queue)/test-budget"
inventory_path="$root/$heavy_inventory_rel"

stream=""
if [ "$#" -ge 1 ]; then
    stream="$1"
    [ -f "$stream" ] || { echo "heavy-derive: no such stream: $stream" >&2; exit 2; }
elif [ -f "$budget_dir/last.json" ]; then
    stream="$budget_dir/last.json"
else
    echo "heavy-derive: no stream given and $budget_dir/last.json is absent." >&2
    echo "heavy-derive: run \`scripts/test-budget.sh ./...\` first (it writes that stream)." >&2
    exit 2
fi

[ -f "$inventory_path" ] || {
    echo "heavy-derive: $heavy_inventory_rel is missing — there is nothing to re-derive against." >&2
    exit 2
}

command -v python3 >/dev/null 2>&1 || { echo "heavy-derive: python3 is required (stdlib json)" >&2; exit 2; }

# A run this script can re-derive from must be a REAL SUITE, and that is a
# coverage claim, not a test count: `make test-budget PKGS=./internal/standards/`
# yields 244 top-level tests (a count floor of 100 would wave it through), while
# `go test ./...` here reports 1677-3553. The stream must therefore report a
# package-level result for EVERY package that carries test files — a whole-suite
# stream, not a slice of one. The count floor stays as the second, cheap guard
# against a truncated stream.
expected_pkgs="$(go list -f "$heavy_golist_format" ./... 2>/dev/null | grep . | paste -sd, -)"
if [ -z "$expected_pkgs" ]; then
    echo "heavy-derive: could not enumerate the packages carrying test files (\`go list\`) — " >&2
    echo "heavy-derive: without that set there is no way to tell a whole-suite stream from a" >&2
    echo "heavy-derive: narrowed one, and deriving from a narrowed run shrinks the classification." >&2
    exit 2
fi

python3 - "$stream" "$inventory_path" "$heavy_slow_threshold_s" "$heavy_stream_tests_floor" "$heavy_gate_packages" "$expected_pkgs" <<'PY'
import json, sys

stream, inventory, floor, tests_floor = sys.argv[1], sys.argv[2], float(sys.argv[3]), int(sys.argv[4])
gate_pkgs = sys.argv[5].split()
expected = {p for p in sys.argv[6].split(",") if p}

rows = {}   # (pkg, test) -> elapsed
suites = {} # (pkg, top-level test) -> summed elapsed
finished = set()  # packages the stream reports a package-level result for
for line in open(stream, errors="replace"):
    line = line.strip()
    if not line.startswith("{"):
        continue
    try:
        ev = json.loads(line)
    except ValueError:
        continue
    action, pkg, test = ev.get("Action"), ev.get("Package", "?"), ev.get("Test")
    if action in ("pass", "fail") and not test:
        finished.add(pkg)
    if action == "run" and test:
        parent = test.split("/", 1)[0]
        suites[(pkg, parent)] = suites.get((pkg, parent), 0.0) + float(ev.get("Elapsed") or 0.0)
    elif action in ("pass", "fail") and test:
        rows[(pkg, test)] = float(ev.get("Elapsed") or 0.0)
    elif action == "skip" and test:
        rows.pop((pkg, test), None)
for key, sec in suites.items():
    if key in rows:
        rows[key] = max(rows[key], sec)

missing = sorted(expected - finished)
if missing:
    print("heavy-derive: the stream reports no result for %d of %d test packages — that is a "
          "narrowed or truncated run, not a suite; refusing to re-derive from it (the candidates "
          "would shrink to whatever that run happened to touch). Missing:" % (len(missing), len(expected)),
          file=sys.stderr)
    for pkg in missing[:10]:
        print("  %s" % pkg, file=sys.stderr)
    if len(missing) > 10:
        print("  … and %d more" % (len(missing) - 10), file=sys.stderr)
    sys.exit(2)

top_level = {(pkg, t): s for (pkg, t), s in rows.items() if "/" not in t}
if len(top_level) < tests_floor:
    print("heavy-derive: the stream carries %d top-level tests (< %d) — that is a "
          "narrowed or truncated run, not a suite; refusing to re-derive from it "
          "(the candidates would shrink to whatever that run happened to touch)"
          % (len(top_level), tests_floor), file=sys.stderr)
    sys.exit(2)

classified = set()
with open(inventory, errors="replace") as fh:
    for line in fh:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) >= 3:
            classified.add((parts[2].rsplit("/", 1)[0], parts[0]))  # (package dir, test)

candidates, gate_hits = [], []
for (pkg, test), sec in sorted(top_level.items(), key=lambda kv: kv[1], reverse=True):
    if sec < floor:
        continue
    if any(g in pkg for g in gate_pkgs):
        gate_hits.append((pkg, test, sec))
        continue
    pkgdir = pkg.split("/gopi/", 1)[-1]
    if (pkgdir, test) in classified:
        continue
    candidates.append((pkg, test, sec))

print("heavy classification floor: %.0fs   (source stream: %s)" % (floor, stream))
print("top-level tests in stream:  %d" % len(top_level))
print("already classified (in %s): %d" % (inventory.split("/")[-1], len(classified)))
print()
if candidates:
    print("CANDIDATES — test<TAB>criterion<TAB>source (pick the criterion by WHY it is heavy;")
    print("it must be one of the heavytest.Criterion* constants):")
    for pkg, test, sec in candidates:
        print("  %7.2fs  %-18s %s" % (sec, pkg.rsplit("/", 1)[-1], test))
else:
    print("CANDIDATES: none — every top-level test at or above the floor is either")
    print("already classified or in a gate package.")
print()
if gate_hits:
    print("EXCLUDED — in a GATE package (may never be heavy; the inventory gate fails on it):")
    for pkg, test, sec in gate_hits:
        print("  %7.2fs  %-18s %s" % (sec, pkg.rsplit("/", 1)[-1], test))
PY
