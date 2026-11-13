package main

// The regression gate for the close-out goroutine sweep's FALSE POSITIVE — the
// one measured 2026-10-22 on this suite and fixed by 01d229e.
//
// THE DEFECT, in one sentence: a run whose only test PASSED still exited 1,
// because the close-out's sweep counted testutil's lifecycle signal arm
// (armSuiteRunRootReapOnSignal, parked on its channel BY DESIGN for the life of
// the process) as a leaked goroutine of THIS repository:
//
//	--- PASS: TestSuiteNotifyEnvIsHermetic (0.00s)
//	suite goroutine sweep: parked repository goroutines: 1
//	  testutil.armSuiteRunRootReapOnSignal.func1()  suiterootsignal.go:77
//	FAIL	github.com/nick/gopi/cmd/agent	1.454s
//
// The consequence is the one that actually hurts: an exit code that stops
// carrying a signal. Green and red stop being distinguishable, so a real
// breakage ("my change broke X") and the suite's own jitter look identical, and
// every reader has to re-derive which half they are looking at.
//
// WHY A RUNTIME GATE, when internal/testutil's join gate is a SOURCE gate: the
// join gate pins the ORDER of the two call sites (join before sample), which is
// necessary and not sufficient — it can hold while the sweep still reports a
// false positive from a THIRD goroutine nobody joined yet (the resident scratch
// sweep was exactly that, one instrument earlier). The property the operator
// cares about is the OBSERVABLE one: "a selector that must pass yields a package
// verdict of PASS". So this gate runs the real binary as a real child and reads
// its real exit code and its real close-out report.
//
// THE TWO-ARM SHAPE, and why the second arm is not decoration:
//
//	arm 1  a real test that must pass        → exit 0 AND the sweep's own line
//	                                          says `parked repository goroutines: 0`
//	arm 2  a selector matching no test       → exit 0
//
// Arm 1 carries the anti-vacuum assertion. `go test` prints the test binary's
// stderr (where the sweep reports) ONLY under -v — measured on this tree: with
// `-run TestSuiteNotifyEnvIsHermetic` and no -v, the whole output is one line,
// `ok github.com/nick/gopi/cmd/agent 1.402s`, so "the log carries no
// `suite goroutine sweep` line" is satisfied by a run whose sweep never spoke at
// all. A gate that grepped for the absence of a line would therefore pass over a
// broken *and* over a silent sweep. This one demands the line, and demands the
// number in it be 0.
//
// Arm 2 pins the other half of "the verdict carries a signal": a mistyped or
// emptied selector must not read as a failure, or an agent that filters wrong
// learns the wrong lesson from a red it caused itself.
//
// COST, measured on this host: ~3s per arm (cold build already warm from the
// parent suite's own run). Rule 27 is not touched: this spawns a filtered run of
// ONE test, not a whole-tree suite, and it is branch-scoped by construction (the
// selector names the guard test).
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/heavytest"
)

const (
	// sweepGatePassingSelector names a test that must pass on any healthy tree:
	// it asserts the suite's own hermeticity pins, so a red there is a real
	// finding about the suite, never about the caller's change.
	sweepGatePassingSelector = "^TestSuiteNotifyEnvIsHermetic$"

	// sweepGateEmptySelector matches no test at all — the empty-filter arm.
	sweepGateEmptySelector = "^TestNoSuchTestZZZ$"

	// sweepGateSweepLine is the close-out sweep's OWN report line. It is the
	// marker this gate requires to be PRESENT: a run that never printed it did
	// not measure, and its exit code proves nothing about the sweep (see the
	// anti-vacuum note above). Named rather than typed twice (Rule 1).
	sweepGateSweepLine = "suite goroutine sweep: parked repository goroutines: 0"

	// sweepGatePassMarker is the per-test PASS line. The package-level `ok` line
	// alone is not enough: a run whose tests were all skipped also prints `ok`.
	sweepGatePassMarker = "--- PASS: TestSuiteNotifyEnvIsHermetic"

	// sweepGateFailMarker is the package-level failure line Go prints. It is
	// asserted ABSENT even when the exit code is 0 — the sweep writes the exit
	// code through TestMain, and a future refactor that stopped doing so would
	// leave a FAIL line above a zero exit (the exact shape this whole bug was).
	sweepGateFailMarker = "FAIL"
)

// TestSuiteGreenRunExitsCleanAndReportsZeroParkedGoroutines is the minimal repro
// gate for the 2026-10-22 false positive: a selector that MUST pass has to yield
// a package verdict of PASS, and the close-out sweep has to SAY it measured zero.
func TestSuiteGreenRunExitsCleanAndReportsZeroParkedGoroutines(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	root := sweepGateRepoRoot(t)

	t.Run("a passing selector yields PASS and a zero-parked sweep", func(t *testing.T) {
		out, code := sweepGateRunChild(t, root, sweepGatePassingSelector)
		if code != 0 {
			t.Fatalf("`go test -v ./cmd/agent -run %s` exited %d, want 0 — a selector that must pass "+
				"cannot produce a red package verdict; the suite's own close-out is reporting work "+
				"nobody leaked\n%s", sweepGatePassingSelector, code, out)
		}
		if !strings.Contains(out, sweepGatePassMarker) {
			t.Errorf("the run never printed %q — a green exit code over a selector that ran nothing "+
				"proves nothing about the sweep\n%s", sweepGatePassMarker, out)
		}
		if !strings.Contains(out, sweepGateSweepLine) {
			t.Errorf("the run did not print the close-out sweep's own line %q.\n"+
				"Either the sweep reported a NON-ZERO count (the false positive is back, or a real "+
				"leak landed), or it never spoke at all — and a silent sweep is the vacuum this gate "+
				"exists to refuse: with no -v the binary's stderr is swallowed, so 'the log has no "+
				"parked line' is satisfied by a run that measured nothing\n%s",
				sweepGateSweepLine, out)
		}
		if strings.Contains(out, sweepGateFailMarker) {
			t.Errorf("a run with exit code %d printed %q — the sweep's verdict and the run's verdict "+
				"disagree, which is the exact defect this gate reproduces\n%s", code, sweepGateFailMarker, out)
		}
	})

	t.Run("an empty selector yields PASS", func(t *testing.T) {
		out, code := sweepGateRunChild(t, root, sweepGateEmptySelector)
		if code != 0 {
			t.Errorf("`go test ./cmd/agent -run %s` (matching nothing) exited %d, want 0 — a mistyped "+
				"filter must not read as a failure, or a reader learns the wrong lesson from a red "+
				"it caused itself\n%s", sweepGateEmptySelector, code, out)
		}
	})
}

// sweepGateRunChild runs the suite in a CHILD process and returns its combined
// output with the exit code, reading the code from the executor rather than from
// a shell pipeline (a pipeline's status belongs to the last stage — the measured
// trap that made a failed command read as success).
func sweepGateRunChild(t *testing.T, root, selector string) (string, int) {
	t.Helper()
	// -v is REQUIRED, not cosmetic: it is what makes the test binary's stderr
	// (the sweep's report) reach this reader at all. -count=1 defeats the result
	// cache, without which a cached PASS would answer for a tree it never ran.
	cmd := exec.Command("go", "test", "-v", "-count=1", "-run", selector, "./cmd/agent")
	cmd.Dir = root
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	if err == nil {
		return out, 0
	}
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) {
		// A failure to RUN is not a verdict about the tree: report it as such
		// rather than folding it into "the selector went red" (Rule 15).
		t.Fatalf("spawn `go test -v -count=1 -run %s ./cmd/agent` in %s: %v\n%s", selector, root, err, out)
	}
	return out, exitErr.ExitCode()
}

// sweepGateRepoRoot walks up from this package's directory to the module root
// (the directory holding go.mod), which is what a `./cmd/agent` pattern must be
// resolved against — resolved from the package directory, the pattern becomes
// ./cmd/agent/cmd/agent and the child reports a build failure instead of a
// verdict.
func sweepGateRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve this package's directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s — the gate cannot locate the module root", dir)
		}
		dir = parent
	}
}
