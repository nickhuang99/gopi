package testutil

// The regression guard for the COMPILE-ONLY exemption (suitepackage_runless.go).
//
// THE DEFECT (measured 2026-11-08, row 01a1b996): the delivery loop's test-compile
// leg is `go test -run '^$' -count=1 ./...`. `-run '^$'` runs no TEST BODY but
// TestMain still runs, so every suite took the cross-run package lock there and the
// leg was REFUSED (exit 99) whenever a sibling run of the same package was in
// flight — a refusal whose premise (shared scratch home, TCP surface, operator
// ledger) a compile-only run cannot reach.
//
// TWO ARMS, and they answer different questions:
//
//	① the DETECTION is right, on every argv shape the toolchain can produce
//	   (pure: no child process, so it is cheap enough to run everywhere);
//	② the REAL TestMain path ANNOUNCES that it takes no lock — the observable
//	   effect of the fix, in the process a `-test.run=^$` invocation actually
//	   starts. Arm ② runs a compiled copy of THIS package's test binary, so it
//	   exercises the same TestMain → PinSuiteForTestMain → AcquireSuitePackageLock
//	   chain the delivery leg does, not a re-implementation of it.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunNothingFilterIsDetectedInArgv(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"equals form (what the toolchain emits)", []string{"suite.test", "-test.run=^$", "-test.timeout=0"}, true},
		{"separate form", []string{"suite.test", "-test.run", "^$"}, true},
		{"compile-only beside other flags", []string{"suite.test", "-test.paniconexit0", "-test.run=^$"}, true},
		{"a named test", []string{"suite.test", "-test.run=TestSomething"}, false},
		{"a named test, separate form", []string{"suite.test", "-test.run", "TestSomething"}, false},
		{"no filter at all (a plain suite run)", []string{"suite.test"}, false},
		{"an anchored pattern that matches a test", []string{"suite.test", "-test.run=^TestFoo$"}, false},
		{"the flag with no value", []string{"suite.test", "-test.run"}, false},
		{"a value that merely CONTAINS the filter", []string{"suite.test", "-test.run=x^$"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runNothingRequestedIn(c.args); got != c.want {
				t.Fatalf("runNothingRequestedIn(%q) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

// TestCompileOnlyRunTakesNoPackageLock drives the REAL entry point: it compiles a
// suite's test binary (internal/replay — the smallest package whose TestMain calls
// testutil.RunSuiteMain, so this arm is one compile, not a suite) and starts it with
// the run-nothing filter, then asserts the child ANNOUNCED that it skipped the lock.
//
// The SUBJECT cannot be this package: internal/testutil has no TestMain (it is the
// package that DEFINES the pins, so its own tests call the pieces), and an arm that
// built it would read "no announcement" for a reason that has nothing to do with
// this fix — measured while writing this arm.
//
// The announcement is the assertion rather than the exit code on purpose: a child of
// this process is an ANCESTOR-NESTED suite, which the ladder already lets through
// without a second lock — so an exit-code-only arm would be green with or without
// the fix (the refusal path needs a NON-ancestor sibling). What the exemption
// changes is exactly the sentence the child prints, and that is what this arm reads.
func TestCompileOnlyRunTakesNoPackageLock(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a suite's test binary — skipped in -short")
	}
	bin := filepath.Join(t.TempDir(), "replay.test")
	buildCmd := exec.Command("go", "test", "-c", "-o", bin, runlessArmSubjectPkg)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("compiling %s's test binary failed: %v\n%s", runlessArmSubjectPkg, err, out)
	}
	child := exec.Command(bin,
		"-test.run="+runNothingFilterForTest,
		"-test.timeout=120s",
	)
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the compile-only child failed (exit %v) — a run that executes NO test must not be refused\n%s", err, out)
	}
	if !strings.Contains(string(out), "this run executes NO test (a test-COMPILE leg), so the cross-run package lock is NOT taken") {
		t.Fatalf("the compile-only child did not announce that it skipped the package lock — the exemption is not reaching the real TestMain path\n%s", out)
	}
}

// runlessArmSubjectPkg is the suite the arm above compiles: the smallest package
// whose TestMain arms the pins through testutil.RunSuiteMain, so the arm exercises
// the real chain (TestMain → PinSuiteForTestMain → AcquireSuitePackageLock) for one
// compile instead of a whole suite's tests.
const runlessArmSubjectPkg = "github.com/nickhuang99/gopi/internal/replay"

// runNothingFilterForTest is the `-run` value the arm above passes. It is a const
// so the arm cannot drift from the shape it is testing, and it is deliberately a
// SECOND declaration of `^$` only inside a test: the production spelling stays
// testcompile.RunNothingFilter (Rule 13/14), and this arm's job is to be the
// independent caller that would notice that constant changing meaning.
const runNothingFilterForTest = "^$"
