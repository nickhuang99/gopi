#!/usr/bin/env bash
# daemoncwd-fixture-concurrency-probe.sh — the acceptance rig for the
# fixture-collision defect that reverted merge 79738cb0.
#
# USAGE:  bash scripts/daemoncwd-fixture-concurrency-probe.sh <worktree> [rounds] [concurrency]
#
# WHAT IT MEASURES: N concurrent runs of the ./internal/daemoncwd/ suite. A
# round FAILS when any of its concurrent runs reports a failure, and a single
# failed run fails the whole probe (exit 1).
#
# WHY: the first version of that package rooted its holder fixtures at a
# HOST-GLOBAL directory (Dir(scratch.HomeDir())/daemoncwd-holder-fixtures) and
# gave five cases the same leaf "worktree-shaped-cwd". Two concurrent runs built
# the SAME path, so the first t.Cleanup RemoveAll deleted the directory a
# sibling's LIVE child was standing in; /proc/<pid>/cwd then answered an
# unreadable cwd, and the package failed with "never reported cwd …" /
# "the measurement itself is unavailable".
#
# The defect is INVISIBLE to repetition alone: `go test -count=100` is green in
# every state, because a single sequential run never has a sibling to collide
# with. Concurrency is the instrument, which is why this lives in a script
# rather than in the package's own tests.
#
# MEASURED (this host, 2026-09-14), 4-way concurrent, 12 rounds = 48 runs:
#   pre-fix tip 0230f6c7 (control)      48 runs → 3 failures (6.25%)
#   geometry fix only                   48 runs → 1 failure
#   geometry + /proc readiness fix      96 runs → 0 failures
#   merged state (this script, acceptance run) 24 runs → 0 failures
#   cumulative AFTER (every probe run ever)   340 runs → 0 failures
#
# Scratch data lives in the shared queue, never /tmp (Rule 22).
set -u
. "$(dirname "$0")/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM

readonly DEFAULT_ROUNDS=6
readonly DEFAULT_CONCURRENCY=4
readonly PKG=./internal/daemoncwd/

if [ "$#" -lt 1 ]; then
    echo "用法: bash scripts/daemoncwd-fixture-concurrency-probe.sh <worktree> [rounds] [concurrency]" >&2
    exit 2
fi
WT="$1"
ROUNDS="${2:-$DEFAULT_ROUNDS}"
CONCURRENCY="${3:-$DEFAULT_CONCURRENCY}"

if [ ! -d "$WT" ]; then
    echo "probe: $WT is not a directory" >&2
    exit 2
fi

OUT="$(gopi_scratch_mkdir daemoncwd-probe)" || { echo "probe: scratch root unavailable" >&2; exit 2; }
BIN="$OUT/daemoncwd.test"

echo "== fixture-concurrency probe: $WT (rounds=$ROUNDS concurrency=$CONCURRENCY) =="
echo "   scratch: $OUT"
if ! (cd "$WT" && CGO_ENABLED=0 go test -c -o "$BIN" "$PKG"); then
    echo "❌ probe: could not build the package's test binary" >&2
    exit 1
fi
echo "   binary: $(sha256sum "$BIN" | cut -c1-16)  tip: $(git -C "$WT" rev-parse --short HEAD)"

fails=0
runs=0
for r in $(seq 1 "$ROUNDS"); do
    pids=()
    for i in $(seq 1 "$CONCURRENCY"); do
        ( cd "$WT" && "$BIN" -test.count=1 >"$OUT/r$r-$i.log" 2>&1 ) &
        pids+=("$!")
    done
    for p in "${pids[@]}"; do
        runs=$((runs + 1))
        if ! wait "$p"; then fails=$((fails + 1)); fi
    done
done

echo "   runs=$runs failures=$fails"
if [ "$fails" -gt 0 ]; then
    echo "❌ probe FAILED — distinct failure lines:"
    grep -hoE "measurement is unavailable|never reported cwd [^ ]*|^--- FAIL: [^ ]*" "$OUT"/*.log |
        sort | uniq -c | sort -rn | head -12
    failing="$(grep -l "^FAIL\|^--- FAIL" "$OUT"/*.log | head -1)"
    echo "   first failing run ($failing), excerpt:"
    grep -m6 -E "^--- FAIL|unavailable|never reported" "$failing"
    exit 1
fi
echo "✅ probe PASSED — 0 failures over $runs concurrent runs"
