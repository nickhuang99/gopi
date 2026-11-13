package main

// THE PACKAGE'S OWN WALL-CLOCK BUDGET, declared where the package can enforce it.
//
// THE DEFECT THIS CLOSES. A bare `go test ./cmd/agent` inherits Go's 10-minute
// default, and this package's measured smoke wall is 579.944s (whole-package
// `go test -count=1 -json ./cmd/agent`, 3240 top-level tests, ΣElapsed 575.4s on
// the same run). That is 96.6% of the default — 20.1s of headroom. At that ratio
// two situations are INDISTINGUISHABLE: a run that finished normally and a run
// that Go's alarm killed mid-suite. The declared entry point,
// `scripts/test-budget.sh`, passes `-timeout 45m` and therefore never met the
// problem; the gap existed ONLY for the bare spelling, which is exactly the one
// a developer types by hand.
//
// WHY THE FIX LIVES HERE AND NOT IN A SCRIPT OR A WRAPPER. The budget is a
// property of THIS package's measured demand, and the one place that can state
// it for a bare invocation is the test binary itself. `testing` registers
// `-test.timeout` as an ordinary flag whose value `M.startAlarm` reads INSIDE
// `m.Run()`, so a `TestMain` that runs before `m.Run()` can set it — measured:
// `flag.Lookup("test.timeout").Value.Set("25m")` before `m.Run()` moves the
// alarm, and `flag.Parse()` is required first because `M.Run` parses only
// `if !flag.Parsed()` and the value reads `0s` until that happens.
//
// WHAT IT IS NOT. It never LOWERS a caller's budget: a run that passes
// `-timeout 45m` (which is what `scripts/test-budget.sh` does) keeps 45m —
// `cmd/go` passes the caller's value through as `-test.timeout=45m0s`, and the
// raise is conditional on the effective value being BELOW this constant. It also
// never turns a hang into a pass: the alarm still fires, just at a distance from
// the measured demand that a healthy run can be told apart from a stuck one.
//
// Rule 27: no load is generated to justify any of this. Every number here is a
// `go test -json` wall clock measured on an ambient host.

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"
)

const (
	// suiteScriptTestBudgetPath is the declared entry point, relative to this
	// package's directory (a test's working directory IS the package dir).
	suiteScriptTestBudgetPath = "../../scripts/test-budget.sh"
)

// suiteScriptTestBudgetRe reads the ONE budget constant out of that script.
var suiteScriptTestBudgetRe = regexp.MustCompile(`(?m)^test_budget="([^"]+)"`)

const (
	// suitePackageBudget is how long this package's test binary may run when the
	// caller declared nothing better. Derived from the measured smoke wall, not
	// chosen: 579.944s of demand (3240 top-level tests) at the 60% ratio the
	// epic's acceptance names for a readable verdict gives ~966s, and 25m =
	// 1500s keeps 579.944s at 38.7% of the budget — so a run that doubles under
	// an ordinary loaded host (the 1.5x-3x span scripts/test-budget.sh's own
	// header records) still finishes inside it, while a genuine hang still ends
	// in a bounded time.
	//
	// It is deliberately BELOW scripts/test-budget.sh's `test_budget="45m"`: the
	// declared entry point keeps its own, larger number (it also runs other
	// packages), and this constant is what a BARE run gets. A package that
	// claimed more than the declared entry point would be inventing a budget
	// nobody agreed to.
	suitePackageBudget = 25 * time.Minute

	// goDefaultTestBudget mirrors testing's per-package default, quoted from
	// `go help testflag`: "-timeout d … The default is 10 minutes (10m)." It is
	// referenced by the gate below and by the raise itself, which must fire for
	// exactly this value (that IS the bare `go test` shape).
	goDefaultTestBudget = 10 * time.Minute

	// suiteBudgetEnv is the opt-out, in seconds: an operator debugging a hang by
	// deliberately running on a SHORT budget sets it and the raise is skipped, so
	// the diagnostic shape they asked for is the one they get. Its own reader is
	// this file (Rule 21).
	suiteBudgetEnv = "GOPI_SUITE_PACKAGE_BUDGET_SEC"
)

// declareSuitePackageBudget raises this binary's `-test.timeout` to
// suitePackageBudget when the caller left it at (or below) Go's default, and
// says so on stderr.
//
// It MUST be called from TestMain BEFORE m.Run(): `M.startAlarm` reads the flag
// inside `m.Run`, so a value set afterwards has no effect on the alarm at all.
func declareSuitePackageBudget() {
	if v := os.Getenv(suiteBudgetEnv); v != "" {
		fmt.Fprintf(os.Stderr, "testutil: %s=%s set — the package budget is NOT declared for this run; "+
			"the caller's own -test.timeout stands\n", suiteBudgetEnv, v)
		return
	}
	// Flag parsing is the precondition, not a convenience: the value reads 0s
	// until the flag set is parsed, and a raise against 0s would compare against
	// the wrong number and print a false before/after. `testing` skips its own
	// parse when this has already run (`if !flag.Parsed()`), so this call is the
	// same parse, taken earlier.
	if !flag.Parsed() {
		flag.Parse()
	}
	effective, ok := suiteBudgetEffectiveTimeout()
	if !ok {
		// The flag is absent or carries a value this build cannot read. Report and
		// leave the caller's budget alone — a raise computed from a reading we
		// could not take would be a guess (Rule 15).
		fmt.Fprintf(os.Stderr, "testutil: %s was unreadable — the package budget is NOT declared for this run\n", testTimeoutFlagName)
		return
	}
	if effective >= suitePackageBudget {
		return // the caller already declared a larger budget; never lower it
	}
	f := flag.Lookup(testTimeoutFlagName)
	if f == nil {
		fmt.Fprintf(os.Stderr, "testutil: %s is not registered — the package budget is NOT declared for this run\n", testTimeoutFlagName)
		return
	}
	if err := f.Value.Set(suitePackageBudget.String()); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: raising %s to %s failed (%v) — the caller's budget stands\n",
			testTimeoutFlagName, suitePackageBudget, err)
		return
	}
	fmt.Fprintf(os.Stderr, "testutil: cmd/agent package budget declared: %s raised %s -> %s "+
		"(measured smoke wall 579.944s = %.1f%% of the raised budget; a bare `go test` otherwise gets Go's %s, "+
		"which that wall leaves 20.1s of headroom against)\n",
		testTimeoutFlagName, effective, suitePackageBudget,
		100*579.944/float64(suitePackageBudget/time.Second), goDefaultTestBudget)
}

// suiteBudgetEffectiveTimeout reads the binary's effective `-test.timeout` from
// the flag set, as `cmd/go` passed it. (false) = the value could not be read,
// which is reported rather than assumed (Rule 15) — an unreadable budget must
// never be treated as "0", because 0 means DISABLED to `testing` and a raise
// computed from it would be a guess.
func suiteBudgetEffectiveTimeout() (time.Duration, bool) {
	f := flag.Lookup(testTimeoutFlagName)
	if f == nil {
		return 0, false
	}
	g, ok := f.Value.(flag.Getter)
	if !ok {
		return 0, false
	}
	d, ok := g.Get().(time.Duration)
	if !ok {
		return 0, false
	}
	return d, true
}

// TestSuitePackageBudgetLeavesReadableHeadroom is the gate: it asserts the
// declaration still describes the measurement it was derived from, so a future
// edit cannot quietly restore the 96.6%-of-10m shape.
//
// It pins THREE relations, and each is the one a plausible careless edit breaks:
//
//	suitePackageBudget >  goDefaultTestBudget   (else a bare run is unchanged)
//	suitePackageBudget <= scripts/test-budget.sh's test_budget
//	                                              (the package never claims more
//	                                               than the declared entry point)
//	measured demand    <= 60% of suitePackageBudget
//	                                              (the epic's acceptance ratio —
//	                                               the number that makes "finished"
//	                                               and "was killed" distinguishable)
func TestSuitePackageBudgetLeavesReadableHeadroom(t *testing.T) {
	if suitePackageBudget <= goDefaultTestBudget {
		t.Fatalf("suitePackageBudget is %s, which does not exceed Go's default %s — a bare `go test ./cmd/agent` "+
			"would inherit that default again and spend %.1f%% of it on the measured smoke wall",
			suitePackageBudget, goDefaultTestBudget, 100*579.944/float64(goDefaultTestBudget/time.Second))
	}
	if scriptBudget := suiteScriptTestBudget(t); scriptBudget > 0 && suitePackageBudget > scriptBudget {
		t.Fatalf("suitePackageBudget is %s, which EXCEEDS scripts/test-budget.sh's test_budget=%s: this package "+
			"would claim a longer budget than the declared entry point grants the whole run",
			suitePackageBudget, scriptBudget)
	}
	const (
		// measuredSmokeWall is this package's whole-package smoke wall, read off
		// `go test -count=1 -json ./cmd/agent` (3240 top-level tests, ΣElapsed
		// 575.4s, package Elapsed 579.944s). It is a MEASUREMENT, not a knob.
		measuredSmokeWall = 579944 * time.Millisecond
		// readableHeadroomPercent is the ceiling the epic's acceptance names: the
		// budget must be at least 100/60 of the demand, so a doubled run still
		// finishes and only a stuck one trips the alarm.
		readableHeadroomPercent = 167 // ceil(100/60) as integer percent of demand
	)
	if got := measuredSmokeWall * readableHeadroomPercent / 100; got > suitePackageBudget {
		t.Fatalf("the measured smoke wall %s needs a budget of at least %s (%.0f%% of the demand) to leave a readable "+
			"verdict, but suitePackageBudget is %s — the declaration no longer describes the distribution it governs",
			measuredSmokeWall, got, float64(readableHeadroomPercent), suitePackageBudget)
	}
}

// suiteScriptTestBudget reads `test_budget=` out of scripts/test-budget.sh — the
// ONE declared budget (Rule 2). A read that fails returns 0 and says so; the
// caller treats 0 as "no comparison possible" rather than as a passing number.
func suiteScriptTestBudget(t *testing.T) time.Duration {
	t.Helper()
	raw, err := os.ReadFile(suiteScriptTestBudgetPath)
	if err != nil {
		t.Logf("cannot read %s (%v) — the package budget is not compared against the declared entry point", suiteScriptTestBudgetPath, err)
		return 0
	}
	m := suiteScriptTestBudgetRe.FindSubmatch(raw)
	if m == nil {
		t.Logf("%s carries no `test_budget=\"…\"` line — not compared", suiteScriptTestBudgetPath)
		return 0
	}
	d, err := time.ParseDuration(string(m[1]))
	if err != nil {
		t.Fatalf("scripts/test-budget.sh's test_budget=%q is not a Go duration: %v", m[1], err)
	}
	return d
}
