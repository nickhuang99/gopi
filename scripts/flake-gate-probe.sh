#!/usr/bin/env bash
# flake-gate-probe.sh — the RED-PROOF driver for the order/determinism flake
# catch mechanism (scripts/flake-catch.sh). It builds a THROWAWAY module whose
# only test is a planted map-iteration-order flake, points the REAL mechanism at
# it, and prints what the mechanism said. Two modes:
#
#   plant  the fixture IS order-dependent (it takes the "first" key of a Go map,
#          whose iteration order is randomized) — the mechanism must NAME it;
#   clean  the same fixture with its order dependence removed — the verdict must
#          not move, and the mechanism must name nobody.
#
# WHY A DRIVER AND NOT A UNIT TEST OF THE PARSER: the claim under test is "the
# mechanism NAMES a new order-dependent flake", and only the real script running
# a real `go test -count=N` over a real planted test can establish it. A test
# that re-implemented the comparison would prove the comparison, not the wiring.
#
# RULE 27: nothing here generates device load, pressure, or contention. The
# instrument is REPETITION of the test itself (`go test -count=N`) — the flake's
# verdict moves because Go randomizes map iteration order, not because the host
# was made sick to make it move.
#
# USAGE
#   scripts/flake-gate-probe.sh plant WORKDIR     # expect exit 1 (named a flake)
#   scripts/flake-gate-probe.sh clean WORKDIR     # expect exit 0 (nobody named)
#
# Exit codes: the mechanism's own (0 = nothing moved, 1 = a flaky test was NAMED,
# 3 = failing every repetition, 4 = nothing was measured), 2 = usage error.

set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/flake-threshold.sh
. "$root/scripts/lib/flake-threshold.sh"

mode="${1:-}"
workdir="${2:-}"

if [ "$mode" != "plant" ] && [ "$mode" != "clean" ]; then
    echo "flake-gate-probe: usage: flake-gate-probe.sh {plant|clean} WORKDIR" >&2
    exit 2
fi
if [ -z "$workdir" ]; then
    echo "flake-gate-probe: WORKDIR is required (a throwaway module dir)" >&2
    exit 2
fi

fixture_dir="$workdir/$flake_probe_dir"
mkdir -p "$fixture_dir" || exit 2

# A self-contained module: no requires, no network, no go.sum. GOWORK=off keeps a
# stray workspace file (a go.work above the throwaway dir) from pulling the
# fixture into another module's build list.
cat >"$workdir/go.mod" <<EOF
module ${flake_probe_dir}

go 1.24
EOF

if [ "$mode" = "plant" ]; then
    test_name="$flake_probe_plant_test"
    cat >"$fixture_dir/${flake_probe_dir}_test.go" <<GOEOF
package ${flake_probe_dir}

import (
	"strings"
	"testing"
)

// The PLANT. A Go map iteration order is randomized, so "the first key" is a
// different key on different runs of the SAME binary on the SAME host. Each
// contract below has exactly ONE key without a hyphen, and the fixture asserts
// that the first key it iterates to IS that one — a coin flip on map order, the
// signature this class is defined by.
//
// Three independent order assertions (see the numbers in the body), each about a
// coin flip on map order, so the fixture itself is a coin flip: the mechanism must
// see BOTH outcomes across repetitions, which is the signature of this class.
func ${test_name}(t *testing.T) {
	// THREE independent order assertions, chosen so the fixture sits at p ~= 1/2 per
	// repetition — the MINIMAX point for a repetition-based red proof. MEASURED
	// first-key-has-no-hyphen rates over 40 runs per key count: 2 keys 0.875, 3 keys
	// 0.775, 4 keys 0.55, 6 keys 0.325 (a small map is NOT uniform: Go picks a random
	// start slot inside the bucket and the first-declared key holds slot 0). THREE
	// three-key maps land at 0.775^3 ~= 0.47, which keeps BOTH ways this probe can
	// lie equally unlikely at the repetition count below:
	//   all repetitions pass (a false "no flake")  = P(R)   ~= 1e-7
	//   all repetitions fail (a false "plain bug") = (1-P)^R ~= 2e-6
	// A single-map fixture cannot do that — at 0.875 the false "no flake" needs R=100
	// repetitions to reach what this reaches at 20, and at 0.325 the false "plain
	// bug" is the likely outcome (0.675^20 ~= 3e-4, i.e. the probe would usually
	// report the WRONG verdict).
	contracts := []map[string]string{
		{"canonical": "yes", "alias-a": "no", "alias-b": "no"},
		{"target": "yes", "alt-1": "no", "alt-2": "no"},
		{"identity": "yes", "side-a": "no", "side-b": "no"},
	}
	for _, m := range contracts {
		first := ""
		for k := range m {
			first = k
			break
		}
		if strings.Contains(first, "-") {
			t.Fatalf("map-order dependent: iteration picked alias %q, the contract names the one key without a hyphen", first)
		}
	}
}
GOEOF
else
    test_name="$flake_probe_clean_test"
    cat >"$fixture_dir/${flake_probe_dir}_test.go" <<GOEOF
package ${flake_probe_dir}

import "testing"

// The same fixture with the order dependence REMOVED: the contract asks the map
// for the key by name, so no iteration order decides the verdict. Negative
// control for the red proof.
func ${test_name}(t *testing.T) {
	canonical := map[string]string{"canonical": "yes", "alias-a": "no", "alias-b": "no"}
	if canonical["canonical"] != "yes" {
		t.Fatalf("contract lookup failed: %q", canonical["canonical"])
	}
}
GOEOF
fi

export GOWORK=off
cd "$workdir" || exit 2

echo "flake-gate-probe: mode=$mode  module=$workdir  plant-test=$test_name"
# -json lands INSIDE the throwaway module: the probe leaves no artifact behind in
# the repository or in the ambient temp root (GOPI.md Rule 22).
"$root/$flake_catch_script" -reps "$flake_probe_reps" -json "$workdir/run.json" ./...
exit $?
