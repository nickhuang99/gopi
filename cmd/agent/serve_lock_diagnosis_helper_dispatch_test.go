package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The serve-lock diagnosis rig's re-exec'd helper must run as a FIXTURE, not as
// a run of the suite.
//
// THE DEFECT THIS PINS (measured 2026-11-08, evidence
// ~/.gopi/sessions/01a1ba46-9a8e-7bc2-b867-7d558d96eb28/evidence/). The test
// spawns its holders by re-exec'ing THIS binary with
// `-test.run=TestServeLockDiagnosisHelperProcess`. Reaching that body through
// m.Run means every helper first executes the whole suite entry point
// (testutil.PinSuiteForTestMain → AcquireSuitePackageLock), so each helper takes
// the cross-run PACKAGE lock of `cmd-agent`. The test's own profile pin
// (`t.Setenv("HOME", t.TempDir())`) is inherited by the helpers, so they all
// derive the SAME scratch home — <the test's temp dir>/workspace/gopi-test-scratch
// — and stage 1's holder is alive for up to diagnosisHoldSec (60 s) while stage 2
// spawns its own helper. The second helper therefore found the first one's lock,
// read a LIVE pid that is a SIBLING (both are children of the test binary, so the
// ancestor rule cannot apply) and refused with testutil.SuiteBusyExitCode = 99
// before writing the pid marker the test waits for:
//
//	the lock-holder helper EXITED before publishing its pids at …/serve.lock.fresh.pids
//	— it died with exit status 99. This is a BROKEN FIXTURE, not a slow host
//
// The parent suite was healthy the whole time: one case red, for this reason only.
//
// WHAT THE FIX IS. TestMain dispatches a helper-mode process BEFORE any pin
// (RunServeLockDiagnosisHelperChild), so no helper takes the package lock, pins an
// env, touches a ledger or announces a scratch placement. The helper carries its
// wiring on its own command line and inherits nothing from the suite.
//
// THE ASSERTION is therefore about the ABSENCE of the suite machinery, which is
// what makes this a regression guard rather than a restatement: the child exits 0
// having published its pid marker, and its stderr carries NO line from the suite
// entry point.

// diagnosisSuiteBusyExitCode is the code the defect produced, named through its
// single definition (Rule 13/14) so the failure text can quote it.
const diagnosisSuiteBusyExitCode = testutil.SuiteBusyExitCode

// diagnosisSuiteEntryPointMarkers are the tell-tale lines the suite entry point
// prints. A helper whose stderr carries any of them ran testutil.PinSuiteForTestMain
// — the state that produced exit 99 — so the presence of one is the defect.
func diagnosisSuiteEntryPointMarkers() []string {
	return []string{
		// the scratch placement report (testutil.reportScratchPlacement)
		`suite "` + cmdAgentSuiteName + `": scratch root`,
		// the ledger pin's inherited-ledger notice (testutil.PinSuiteLedgerEnv)
		"is INHERITED (a test named this ledger",
		// the cross-run exclusion, both its refusal and its announcements
		"another run of this package is in flight",
		"the package lock is DISABLED by",
		"nested-suite — continuing WITHOUT taking a second lock",
	}
}

// diagnosisParsePids parses a "<owner> <leak>" marker, failing on anything else
// (a malformed marker means the helper body changed shape, not that the test may
// guess).
func diagnosisParsePids(t *testing.T, line string) (owner, leak int) {
	t.Helper()
	fields := strings.Split(line, diagnosisPidSeparator)
	if len(fields) != 2 {
		t.Fatalf("the owner marker %q must carry two pids separated by %q", line, diagnosisPidSeparator)
	}
	owner, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("owner pid %q in the marker: %v", fields[0], err)
	}
	leak, err = strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("leaked-holder pid %q in the marker: %v", fields[1], err)
	}
	return owner, leak
}

func TestServeLockDiagnosisHelperRunsOutsideTheSuiteEntryPoint(t *testing.T) {
	lockPath := diagnosisLockPath(t)
	marker := lockPath + ".dispatch.pids"

	// die=1: the owner publishes its pids and exits at once, so a healthy helper
	// finishes in milliseconds and this case never waits on a holder it must kill.
	h := startDiagnosisHelper(t,
		diagnosisModeEnv+"="+diagnosisModeHold,
		diagnosisPathEnv+"="+lockPath,
		diagnosisMarkerEnv+"="+marker,
		diagnosisDieEnv+"="+diagnosisDieYes,
	)
	t.Cleanup(h.killAndReap)

	line := diagnosisWaitMarker(t, h, marker)
	owner, leak := diagnosisParsePids(t, line)
	t.Cleanup(func() { diagnosisKill(t, leak); diagnosisKill(t, owner) })

	// The exit STATUS of the helper itself: the fixture that reported 99 is now a
	// clean 0, and the marker above could not exist otherwise.
	select {
	case err := <-h.exited:
		h.drain()
		if err != nil {
			t.Fatalf("the helper did not exit cleanly (%v) — a non-zero status here IS the defect this case exists for: exit %d is testutil.SuiteBusyExitCode, i.e. the fixture took the suite's own cross-run package lock instead of being dispatched before the pins.\nhelper stderr:\n%s",
				err, diagnosisSuiteBusyExitCode, h.stderr.text())
		}
	case <-time.After(diagnosisDrainGrace):
		t.Fatalf("the helper published its pids but did not exit — the die=1 shape returns immediately")
	}

	// The ABSENCE half: not one line of the suite entry point may appear, because
	// a helper that reaches those pins is the defect (and on a host running any
	// sibling suite it is an exit 99 waiting to happen).
	noise := h.stderr.text()
	for _, banned := range diagnosisSuiteEntryPointMarkers() {
		if strings.Contains(noise, banned) {
			t.Errorf("the helper's stderr carries %q — the fixture ran the suite entry point (testutil.PinSuiteForTestMain) instead of being dispatched by TestMain before it.\nhelper stderr:\n%s",
				banned, noise)
		}
	}
}
