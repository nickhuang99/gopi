package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestTaskChildFailGuardRefusesTheRealProfile is the in-process proof for the
// 2026-09-16 configuration itself: HOME-derived profile, escape armed, NO
// throwaway root anywhere. It is read-only by construction — the guard only
// MEASURES — so it can name the operator's real ledger without risking a row
// (no claim, no writeback, no binary spawn).
//
// WHY IT IS THE LOAD-BEARING CASE: `/` is device 2051 on this host while /home
// is 2052, so a predicate that only asked "is this off the `/` device" would
// call the operator's real profile a sandbox and let the escape burn rows again.
// This test fails red on that predicate and green on the temp-root containment
// one (Rule 10.4: the boundary sample, not the typical one).
func TestTaskChildFailGuardRefusesTheRealProfile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory to name: %v", err)
	}
	realLedger := filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)
	// Both DB spellings masked → the ledger falls back to the profile default,
	// which is the real one (GOPI_HOME = the OS home, i.e. the operator shape).
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")
	t.Setenv(taskChildFailTestEnv, "1")

	if got := config.LedgerPath(); got != realLedger {
		t.Fatalf("fixture precondition: ledger resolves to %q, want the operator's real file %q", got, realLedger)
	}
	// Measurement, not spelling: the two devices really are different here, so
	// the containment test is what does the work.
	if ok, err := scratch.IsTempRootOffRootFS(realLedger); err != nil {
		t.Fatalf("measuring the real ledger: %v", err)
	} else if !ok {
		t.Logf("note: this host has %s on the same device as %s, so the device half alone would refuse too",
			realLedger, scratch.RootDeviceProbePath)
	}
	err = refuseIfUnsafeTaskChildFailEscape(taskChildFailVerbRun)
	if err == nil {
		t.Fatalf("an armed escape pointed at the OPERATOR's real profile (%s) must be refused", realLedger)
	}
	for _, want := range []string{taskChildFailTestEnv, "REFUSED", realLedger} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
}

// TestTaskChildFailGuardAdmitsASandboxFixture is the pairing control at unit
// level: the SAME guard, the same armed variable, a throwaway profile under the
// measured temp root → no refusal. Without this control the test above would
// pass on a guard that refused everything (and every hermetic test in this
// package would be the only signal).
func TestTaskChildFailGuardAdmitsASandboxFixture(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	t.Setenv(config.MemoryDBEnv, filepath.Join(sandbox, config.MemoryDBFileName))
	t.Setenv(config.MemoryDBEnvLegacy, "")
	t.Setenv(taskChildFailTestEnv, "1")

	sandboxRoot, err := taskChildFailSandboxRoot()
	if err != nil {
		t.Fatalf("a throwaway profile under the temp root (%s) must be admitted, got: %v", sandbox, err)
	}
	if sandboxRoot != filepath.Join(sandbox, config.MemoryDBFileName) {
		t.Errorf("sandbox root = %q, want the named ledger %q", sandboxRoot, filepath.Join(sandbox, config.MemoryDBFileName))
	}
	if err := refuseIfUnsafeTaskChildFailEscape(taskChildFailVerbWorker); err != nil {
		t.Fatalf("the guard refused a sandbox fixture: %v", err)
	}
}

// TestTaskChildFailGuardUnarmedIsNil pins the "not armed" branch: the guard must
// be a no-op for every ordinary run — a false refusal would stop production work
// just as surely as the original bug failed it.
func TestTaskChildFailGuardUnarmedIsNil(t *testing.T) {
	t.Setenv(taskChildFailTestEnv, "")
	t.Setenv(config.GopiDirEnv, "")
	if err := refuseIfUnsafeTaskChildFailEscape(taskChildFailVerbRun); err != nil {
		t.Fatalf("an UNARMED escape must never refuse, got: %v", err)
	}
}

// TestTaskChildFailRefusalExitCodeMatchesTheRepro pins the one literal the
// E2E repro spells for itself (wantTaskChildFailRefusalExit): the repro must
// compile against the pre-fix tree to be red before the fix, and this assertion
// keeps that literal equal to the shipped constant.
func TestTaskChildFailRefusalExitCodeMatchesTheRepro(t *testing.T) {
	if taskChildFailRefusalExitCode != wantTaskChildFailRefusalExit {
		t.Errorf("taskChildFailRefusalExitCode = %d but the repro asserts %d",
			taskChildFailRefusalExitCode, wantTaskChildFailRefusalExit)
	}
}

// TestTaskChildFailRefusalExitCodeIsDistinct guard-references the refusal code:
// a launcher distinguishes it by value, so a future edit that makes it collide
// with a sibling exit code must fail the build's tests, not an operator's $?.
func TestTaskChildFailRefusalExitCodeIsDistinct(t *testing.T) {
	for _, sibling := range []struct {
		name string
		code int
	}{
		{name: "workerUnexpectedArgExitCode", code: workerUnexpectedArgExitCode},
		// mergerGracefulExitCode WAS here; it belonged to the retired resident
		// merger's stop path (epic daemon-machinery-removal, 2026-09-17). The
		// distinctness guard stays and still covers the surviving siblings.
		{name: "config.AgentGuardRefusalExitCode", code: config.AgentGuardRefusalExitCode},
		{name: "testutil.SuiteScratchUnsafeExitCode", code: testutil.SuiteScratchUnsafeExitCode},
	} {
		if taskChildFailRefusalExitCode == sibling.code {
			t.Errorf("taskChildFailRefusalExitCode = %d collides with %s", taskChildFailRefusalExitCode, sibling.name)
		}
	}
}

// TestReproPredicateMirrorsTheProductionGuard pins the repro's local mirror
// helpers (tempRootsForRepro / pathInAnyTempRootForRepro) to the shipped
// predicate: the repro spells them itself so it compiles against the pre-fix
// tree, and this test keeps that copy equal to the code it stands in for
// (Rule 13 — a copy is forbidden from drifting away from its definition).
func TestReproPredicateMirrorsTheProductionGuard(t *testing.T) {
	production := taskChildFailTempRoots()
	mirror := tempRootsForRepro()
	if len(production) != len(mirror) {
		t.Fatalf("the repro mirrors %d temp root(s), the production predicate %d: %v vs %v",
			len(mirror), len(production), mirror, production)
	}
	for i := range production {
		if production[i] != mirror[i] {
			t.Errorf("temp root %d: repro %q, production %q", i, mirror[i], production[i])
		}
	}
	// Containment must agree on both the inside and the outside case.
	inside := t.TempDir()
	if !pathInAnyTempRootForRepro(inside, mirror) || !pathInAnyRoot(inside, production) {
		t.Errorf("the two containment helpers disagree on the inside case %q", inside)
	}
	outside := rootPartitionForRepro
	if pathInAnyTempRootForRepro(outside, mirror) {
		t.Errorf("the repro's containment says %q is inside a temp root", outside)
	}
}
