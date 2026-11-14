package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A suite's close-out may only MEASURE parked goroutines after it has JOINED the
// lifecycle goroutines this package arms. The signal arm is parked on its channel
// for the whole life of the process BY DESIGN (it exists to reap the run root if
// a SIGTERM ends the run), so a close-out that samples before joining it reports
// a leak that is the instrument itself:
//
//	suite goroutine sweep: parked repository goroutines: 1
//	  testutil.armSuiteRunRootReapOnSignal.func1()  suiterootsignal.go:77
//	FAIL	github.com/nickhuang99/gopi/cmd/agent	1.454s      (a run whose only test PASSED)
//
// measured 2026-10-22 on `go test ./cmd/agent/` — green tests, red run, and an
// exit code that stops carrying a signal.
//
// WHY A SOURCE GATE AND NOT A RUNTIME ARM: the arm is PROCESS-global, so a test
// that exercised the join here would disarm the very handler this suite relies on
// for a signal arriving later in its own run. The order is a property of the two
// call sites, and it is that property the gate pins — the runtime proof (a green
// run reporting `parked repository goroutines: 0`) belongs to the close-out's own
// run and is cited in the task report.
func TestSuiteCloseOutJoinsTheSignalArmBeforeSampling(t *testing.T) {
	root := repoRootForGate(t)
	for _, c := range []struct {
		name string
		path string
		join string
		then string
	}{
		{
			name: "cmd/agent close-out joins before it samples",
			path: filepath.Join("cmd", "agent", "testmain_test.go"),
			join: "testutil.JoinSuiteRunRootSignalHandler()",
			then: "reportSuiteGoroutinesAtCloseOut()",
		},
		{
			name: "FinishSuiteForTestMain joins before it removes the root",
			path: filepath.Join("internal", "testutil", "scratchroot.go"),
			join: "JoinSuiteRunRootSignalHandler()",
			then: "CleanupSuiteTempRoot()",
		},
	} {
		src := readGateFile(t, root, c.path)
		if reason := joinPrecedesSample(src, c.join, c.then); reason != "" {
			t.Errorf("%s: %s", c.name, reason)
		}
	}
	// The ordering checker itself must be able to go RED, or the two assertions
	// above are a green over a vacuum (a checker that always says "fine" is the
	// shape this repository has been bitten by: a named check that measures
	// nothing).
	redArm := "func TestMain(m *testing.M) {\n\tcode := m.Run()\n\treportSuiteGoroutinesAtCloseOut()\n\ttestutil.JoinSuiteRunRootSignalHandler()\n}\n"
	if joinPrecedesSample(redArm, "testutil.JoinSuiteRunRootSignalHandler()", "reportSuiteGoroutinesAtCloseOut()") == "" {
		t.Error("the ordering checker did not detect a sample taken BEFORE the join — it is a green over nothing")
	}
	missingArm := "func TestMain(m *testing.M) { os.Exit(m.Run()) }\n"
	if joinPrecedesSample(missingArm, "testutil.JoinSuiteRunRootSignalHandler()", "reportSuiteGoroutinesAtCloseOut()") == "" {
		t.Error("the ordering checker did not report a MISSING join — an absent join is the defect, not a non-event")
	}
}

// joinPrecedesSample returns "" when join appears before sample in src, and the
// reason it does not otherwise. It is pure so the red arm above can call it.
func joinPrecedesSample(src, join, sample string) string {
	i, j := strings.Index(src, join), strings.Index(src, sample)
	switch {
	case i < 0:
		return "the close-out does not call " + join + " at all — the signal arm stays parked when the dump is taken"
	case j < 0:
		return "the close-out does not call " + sample + " at all — the gate is looking at a file that no longer measures"
	case i > j:
		return "the dump is taken BEFORE " + join + " — the signal arm is counted as a leak of this repository"
	}
	return ""
}

// repoRootForGate resolves the repository root from this package's directory.
func repoRootForGate(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve this package's directory: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatalf("resolve the repository root from %s: %v", wd, err)
	}
	return root
}

// readGateFile reads a tracked source file the gate inspects, failing with the
// path rather than an empty string (a gate that silently reads nothing passes).
func readGateFile(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s (relative to %s): %v", rel, root, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s read as EMPTY — the gate cannot judge an empty file", rel)
	}
	return string(raw)
}
