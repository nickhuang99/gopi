#!/bin/bash
# GoPi Smoke Test Suite — runs all test inputs, verifies they complete successfully.
# Usage: GOPI=./bin/gopi bash testdata/smoke.sh
set -e

GOPI=${GOPI:-./bin/gopi}
PASS=0
FAIL=0

run_test() {
    local name="$1"
    local input="$2"
    local max_iter="$3"
    echo -n "  $name ... "
    if output=$("$GOPI" "$input" 2>&1); then
        if echo "$output" | grep -q 'Error:'; then
            echo "FAIL (agent error)"
            echo "$output" | grep 'Error:' | head -3
            FAIL=$((FAIL + 1))
        else
            echo "PASS"
            PASS=$((PASS + 1))
        fi
    else
        rc=$?
        echo "FAIL (exit $rc)"
        FAIL=$((FAIL + 1))
    fi
}

echo "=== GoPi Smoke Tests ==="
echo "Binary: $GOPI"
echo ""

run_test "Fibonacci Python"   testdata/gopi-test-input.md
run_test "Fibonacci C"        testdata/gopi-c-input.md
run_test "Fibonacci C++ Edit" testdata/gopi-cpp-edit-input.md
run_test "Edit Tool"          testdata/gopi-edit-input.md
run_test "Large File Search"  testdata/gopi-bigfile-test.md
run_test "Diary Search"       testdata/gopi-diary-search.md
run_test "DeepSeek Doc"       testdata/gopi-deepseek-doc-prompt.md

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && echo "ALL PASS" || echo "SOME FAILED"
exit $FAIL
