package signalrec

// signalrec_fixturebinding_test.go — the wiring that binds this suite's victim
// children to the test binary's lifetime.
//
// WHY THIS SUITE IS THE ONE THAT HAD TO BE WIRED. The leak the epic measured was
// a `signalrec.test` orphan (pid 1525157) standing in the task worktree
// `gopi-01a0d68746ed7ea090e56bff794e1b9f` long after its suite was gone, so
// finish-cleanup's holder guard refused to release the directory and the branch
// could not be deleted. The victims below were the leak: they blocked in
// `select {}` waiting to be SIGTERMed by a parent that no longer existed.
//
// THREE THINGS, and the suite needs all three (see internal/testutil/
// fixturelifetime.go for the mechanism itself):
//
//	the ARM      — TestMain calls testutil.ArmAndSweepForSuite, so the suite
//	               process holds the write end of the death pipe alone.
//	the ADOPTION — every victim child calls srBindThisFixtureChild before it
//	               announces readiness, so the kernel killing the holder takes
//	               the child with it (SIGKILL included).
//	the PROOF    — the parent runs the guard the bind handed it after the
//	               child's readiness handshake, so a case whose child failed to
//	               bind FAILS instead of passing for the wrong reason.

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// srFixtureBindingExitCode is the exit status of a victim child that could
	// not bind itself. Its binding verdict (armed=false) is the machine-readable
	// statement; this code is the human-facing one, and neither is silent.
	srFixtureBindingExitCode = 1
)

// srBindThisFixtureChild is the CHILD side: it adopts the inherited death pipe
// and reports the verdict, BEFORE the child announces readiness — so the
// parent's first read of the report already carries the answer, and a child that
// could not bind never masquerades as a live victim.
//
// It exits non-zero when the binding is not in force. A victim that runs unbound
// is exactly the orphan this epic is about: better a red case than a leak.
func srBindThisFixtureChild() {
	if err := testutil.WatchFixtureBinding(); err != nil {
		fmt.Fprintf(os.Stderr, "signalrec: victim child pid %d is NOT bound to its test binary: %v\n", os.Getpid(), err)
		os.Exit(srFixtureBindingExitCode)
	}
}

// srBindVictim is the PARENT side of the wiring, and the ONE place a case in
// this suite hands its victim the binding. It MUST be called after the command's
// environment is complete (the binding is part of that environment) and before
// Start.
//
// It returns the guard to run once the child has announced readiness: the
// verdict can only be read after the child has run its arming code, so the
// caller runs it right after srAwaitChildReady.
func srBindVictim(t *testing.T, cmd *exec.Cmd, name string) func() {
	t.Helper()
	report := testutil.FixtureBindingReportPath(t, name)
	if err := testutil.BindFixtureChild(cmd, report); err != nil {
		t.Fatalf("bind the victim child %q to this test binary's lifetime: %v", name, err)
	}
	return func() { testutil.RequireFixtureChildBinding(t, report) }
}
