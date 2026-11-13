package testutil

// Regression guard for the tested-binary pin (measured 2026-10-03, task
// 01a1023c). GopiBin used to fall through to <module root>/bin/gopi, where the
// module root came from walking up the TEST's own CWD: a shared checkout
// therefore let a worktree bin/gopi built from ANOTHER revision decide an e2e
// case's verdict. Measured consequence: the same revision of
// cmd/agent's TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow is green
// standalone and inside one two-package budget run, then RED inside the next,
// because the binary reached through that path predated commit 1d901244 and the
// run reported the OLD binary's production refusal as a flake of the tree under
// test.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGopiBinPrefersTheSuitePinnedBinary is the pin's arm: the caller's own
// build (GOPI_BIN) outranks the suite pin, the suite pin outranks the tree's
// bin/gopi, and an explicitly named binary that does not exist is FATAL rather
// than a silent substitution (Rule 15).
func TestGopiBinPrefersTheSuitePinnedBinary(t *testing.T) {
	dir := t.TempDir()
	caseBin := filepath.Join(dir, "case-built-gopi")
	suiteBin := filepath.Join(dir, "suite-built-gopi")
	for _, p := range []string{caseBin, suiteBin} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// The suite pin alone is enough: it is what a multi-package run must hand
	// every package, and it must win over anything reachable from the CWD.
	t.Setenv(BinaryPinEnv, "")
	t.Setenv(SuiteBinEnv, suiteBin)
	if got := GopiBin(t); got != suiteBin {
		t.Fatalf("with only %s set, GopiBin returned %q, want %q — a suite that resolves the tree's "+
			"bin/gopi verifies a revision its runner never built", SuiteBinEnv, got, suiteBin)
	}

	// A single case's own build still outranks the suite pin.
	t.Setenv(BinaryPinEnv, caseBin)
	if got := GopiBin(t); got != caseBin {
		t.Fatalf("with %s set, GopiBin returned %q, want %q — a case that built its own binary must "+
			"not be handed the suite's", BinaryPinEnv, got, caseBin)
	}
}

// TestGopiBinPublishesTheTreeBuildToChildren pins the second half of the fix:
// when the suite build path runs, the pinned path must be published through
// GOPI_TEST_BIN so a CHILD process the suite spawns resolves the same revision
// instead of building or hunting one of its own.
//
// The case is about the SUITE build arm, so it holds only when that arm is the
// one that resolves; see the precondition below for the other legitimate state.
func TestGopiBinPublishesTheTreeBuildToChildren(t *testing.T) {
	// A binary ALREADY resolved by a higher-precedence pin is a legitimate
	// state in which the suite build never runs: $BinaryPinEnv (a single case's
	// own build) is resolved at arm 1 of resolveGopiBin and outranks the suite
	// pin BY DESIGN — TestGopiBinPrefersTheSuitePinnedBinary above asserts
	// exactly that precedence. With it set, resolution returns before the
	// suite-build arm, so nothing is ever published to $SuiteBinEnv and this
	// case's contract ("the SUITE build publishes its result") is not the
	// question the run is asking. Measurement of the alternative: asserting
	// publication here would require DEMOTING the caller's pin (so this package
	// would build a second binary the run never asked for) — the case would
	// then contradict the case above instead of observing the same contract.
	// The skip is therefore the honest verdict, not a relaxed assertion: a PASS
	// in this state would be a green claiming a build that provably did not run.
	if p := os.Getenv(BinaryPinEnv); p != "" {
		if _, err := os.Stat(p); err == nil {
			t.Skipf("%s=%s already resolved the tested binary, and it outranks the suite pin by design — "+
				"the suite build arm (and its %s publication) is unreachable in this run", BinaryPinEnv, p, SuiteBinEnv)
		}
	}
	if !SuiteTempRootPinned() {
		t.Skip("scratch root is not pinned here, so the tree build is off by design")
	}
	t.Setenv(SuiteBinEnv, "")
	before := os.Getenv(SuiteBinEnv)

	got := GopiBin(t)
	if got == "" {
		t.Fatal("GopiBin returned an empty path")
	}
	if os.Getenv(SuiteBinEnv) == "" {
		t.Fatalf("GopiBin returned %q but left %s unset (was %q before the call) — a child process "+
			"spawned by the suite would resolve its own binary, so the run would no longer verify one "+
			"revision", got, SuiteBinEnv, before)
	}
	if _, err := os.Stat(os.Getenv(SuiteBinEnv)); err != nil {
		t.Fatalf("the published tested binary %q is not usable: %v", os.Getenv(SuiteBinEnv), err)
	}
}

// publishPinCaseName is the case the two guards below re-execute; it is named
// once (Rule 13) so a rename cannot silently leave the fixture driving a case
// that no longer exists — the child would then report "no tests to run" and the
// assertion on its output would say so instead of passing vacuously.
const publishPinCaseName = "TestGopiBinPublishesTheTreeBuildToChildren"

// gopiBinPinCaseEnv marks the fixture CHILD of
// TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved: the child re-runs the
// publish case ALONE with a usable $BinaryPinEnv, so the parent can assert the
// case's OUTCOME in that state.
const gopiBinPinCaseEnv = "GOPI_TESTUTIL_PUBLISH_PIN_FIXTURE"

// TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved is the regression guard
// for the state the two cases above must agree on. Measured 2026-10-04: with
// $BinaryPinEnv set (the shape every e2e run that needs a binary produces),
// TestGopiBinPublishesTheTreeBuildToChildren FAILED, because it asserted a
// publication that the higher-precedence arm makes unreachable — so
// `go test ./internal/testutil/` read as a package failure and hid the package's
// real regressions.
//
// It re-executes this test binary, because the case's outcome (skip vs fatal) is
// not observable in-process: the parent asserts on the CHILD's verdict instead.
func TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved(t *testing.T) {
	if os.Getenv(gopiBinPinCaseEnv) != "" {
		// Fixture arm: this guard is not the subject of the child run.
		t.Skip("fixture arm: the guard runs the publish case as a child process")
	}
	dir := t.TempDir()
	caseBin := filepath.Join(dir, "case-built-gopi")
	if err := os.WriteFile(caseBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+publishPinCaseName+"$", "-test.v")
	cmd.Env = append(os.Environ(),
		gopiBinPinCaseEnv+"=1",
		BinaryPinEnv+"="+caseBin,
		SuiteBinEnv+"=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s FAILS when %s already resolved the tested binary — a run that hands the suite its own "+
			"build (e2e) then reads as a package failure and hides this package's real regressions. "+
			"Child output:\n%s", publishPinCaseName, BinaryPinEnv, out)
	}
	if !strings.Contains(string(out), "--- SKIP: "+publishPinCaseName) {
		t.Fatalf("%s did not SKIP in the %s-pinned state. A PASS there would claim a suite build that the "+
			"higher-precedence arm never ran (Rule 15: a green that proves nothing). Child output:\n%s",
			publishPinCaseName, BinaryPinEnv, out)
	}
}

// The no-silent-substitution arm (a pin naming an absent path FAILS the case
// rather than falling back to a binary of unknown revision) is asserted by
// TestGopiBinFailsLoudlyOnAnUnusablePinFixture, which runs THIS package's test
// binary as a subprocess: a t.Fatalf arm cannot be asserted in-process without
// failing the suite that asserts it.
const gopiBinAbsentPinEnv = "GOPI_TESTUTIL_ABSENT_PIN_FIXTURE"

// TestGopiBinFailsLoudlyOnAnUnusablePin is the fixture's runner: it re-executes
// this test binary with a suite pin naming a path that does not exist, and
// requires the child to FAIL (a silent fallback is the defect the pin removes).
func TestGopiBinFailsLoudlyOnAnUnusablePin(t *testing.T) {
	if os.Getenv(gopiBinAbsentPinEnv) == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGopiBinFailsLoudlyOnAnUnusablePin$", "-test.v")
		cmd.Env = append(os.Environ(),
			gopiBinAbsentPinEnv+"=1",
			BinaryPinEnv+"=",
			SuiteBinEnv+"="+filepath.Join(os.TempDir(), "testutil-absent-gopi-never-created"),
		)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("the child run with %s naming an absent path PASSED — GopiBin fell back to a binary "+
				"whose revision this run does not control. Output:\n%s", SuiteBinEnv, out)
		}
		if !strings.Contains(string(out), "not usable") {
			t.Fatalf("the child failed for the WRONG reason (want an unusable-pin message):\n%s", out)
		}
		return
	}
	// Fixture arm: must not survive.
	if got := GopiBin(t); got == "" {
		t.Fatalf("expected a fatal on an unusable %s pin", SuiteBinEnv)
	}
}
