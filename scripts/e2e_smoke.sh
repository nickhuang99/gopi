#!/usr/bin/env bash
# e2e_smoke.sh — one-click three-mode E2E smoke suite for gopi.
#
# Builds bin/gopi, then runs testsuite/ (single task + chat + web) against a
# scripted mock LLM with a sandboxed GOPI_HOME. ZERO real API calls and no key
# required: the suite passes in a keyless/offline shell. This is the safety net
# to run before/after any large change (web alignment, task dispatcher, bug
# flow) — see devdoc/testsuite-skeleton.md.
#
# Usage:
#   scripts/e2e_smoke.sh                 # build + full three-mode smoke
#   scripts/e2e_smoke.sh -run TestTask_  # extra flags are passed to go test
#
# Exit code: 0 = all green, 1 = build or test failure.

set -uo pipefail

# Smoke budget: the whole suite is mock-backed (sub-second rounds), so 15 min
# is generous headroom over the observed ~40 s — it only trips on a hang.
smoke_timeout="15m"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root" || exit 1

# Scratch root under the shared workspace scratch home — never /tmp, which is
# the 87%-full / partition behind the 2026-09-11 exhaustion. The smoke's go
# test log and TMPDIR both come from here. See scripts/lib/tmp-ban.sh.
# shellcheck source=lib/tmp-ban.sh
. "$root/scripts/lib/tmp-ban.sh"
SCRATCH="$(gopi_scratch_mkdir smoke)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
if gopi_dry_run_requested "$@"; then
    gopi_dry_run_report e2e_smoke "$SCRATCH" \
        "build bin/gopi then run the three-mode testsuite (single task / chat / web) against the mock LLM" \
        go
    exit $?
fi
# --dry-run is consumed here, not forwarded to `go test`.
TEST_ARGS=()
for arg in "$@"; do
    [ "$arg" = "--dry-run" ] && continue
    TEST_ARGS+=("$arg")
done
export TMPDIR="$SCRATCH/tmp"   # the suite's own scratch stays off `/` too
mkdir -p "$TMPDIR"

echo "=== gopi e2e smoke ==="
echo "root: $root"

echo "--- [1/2] build bin/gopi ---"
if ! CGO_ENABLED=0 go build -buildvcs=false -o bin/gopi ./cmd/agent; then
    echo "FAIL: build ./cmd/agent" >&2
    exit 1
fi
echo "built: $root/bin/gopi"

    echo "--- [2/2] testsuite (mock LLM, sandboxed GOPI_HOME) ---"
echo "    scratch: $SCRATCH (off the / filesystem)"
export GOPI_BIN="$root/bin/gopi"
log="$SCRATCH/smoke.log"
status=0
go test ./testsuite/ -count=1 -v -timeout "$smoke_timeout" ${TEST_ARGS[@]+"${TEST_ARGS[@]}"} >"$log" 2>&1 || status=$?

# Per-mode report: the three mode families are named by prefix.
pass_task=$(grep -c '^--- PASS: TestTask_' "$log")
fail_task=$(grep -c '^--- FAIL: TestTask_' "$log")
pass_chat=$(grep -c '^--- PASS: TestChat_' "$log")
fail_chat=$(grep -c '^--- FAIL: TestChat_' "$log")
pass_web=$(grep -c '^--- PASS: TestWeb_' "$log")
fail_web=$(grep -c '^--- FAIL: TestWeb_' "$log")

cat "$log"
rm -f "$log"

echo "=== smoke report ==="
printf 'single task : %d pass / %d fail\n' "$pass_task" "$fail_task"
printf 'chat REPL   : %d pass / %d fail\n' "$pass_chat" "$fail_chat"
printf 'web (SSE)   : %d pass / %d fail\n' "$pass_web" "$fail_web"

if [ "$status" -ne 0 ]; then
    echo "RESULT: FAIL (go test exit $status)"
    exit 1
fi
echo "RESULT: PASS"
