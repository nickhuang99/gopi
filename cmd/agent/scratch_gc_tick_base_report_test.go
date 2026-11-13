package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// THE RESIDENT TICK MUST NOT DELETE THE SHARED BASE'S SUITE ROOTS.
//
// The default memory base is /dev/shm: WORLD-WRITABLE, shared with every other
// process on the host, and exactly where internal/testutil's placement puts a
// running suite's per-run root (`suite-<pkg>-run-<n>`). base_suite_runs.go
// therefore reserves that family for the OPERATOR's one-shot verb
// (`gopi tool scratch-gc base`) and says so in its header: "an automatic pass over it
// would delete the roots of the suites executing on the same host".
//
// It said so while the code did the opposite. cmd/agent/scratch_gc_tick.go
// called scratch.SweepBaseSuiteRuns — the EXPLICIT arm, which never consults the
// automatic stand-down and calls removeCollectable unconditionally — on every
// tick, with a comment two lines below claiming "The resident tick does NOT
// delete this family". The tick ate them: measured on this host 2026-10-01, 16
// aged roots per hour, and /dev/shm held none, which is also why the
// 2026-09-28 leak (146 roots / 5.9G of a 7.8G tmpfs) can re-form invisibly
// behind the deletion.
//
// This test is the regression guard, and it drives the REAL surface: the tick
// itself, through the function the worker's round calls (cli_tasks_worker.go
// calls scratchGCSweep). It is RED on the pre-fix code — the aged root is gone
// after one tick — and GREEN after.
func TestScratchGCTickReportsAndDoesNotDeleteBaseSuiteRoots(t *testing.T) {
	home := t.TempDir()
	// The suite stand-down is the state EVERY `go test` run has, and it must stay
	// in force here too: it is the stand-down of the AUTOMATIC namespace, and the
	// explicit arm must not be reachable around it (the whole point of the fix).
	prev := scratch.SetAutoBaseResidue(false)
	defer scratch.SetAutoBaseResidue(prev)

	// A base of this test's OWN, off `/` (Rules 22/32: a fixture never touches
	// the operator's real /dev/shm, and never saturates it — Rule 27).
	base := t.TempDir()
	aged := filepath.Join(base, "suite-cmd-agent-run-3113361481")
	fresh := filepath.Join(base, "suite-tools-run-9876543210")
	for _, dir := range []string{aged, fresh} {
		if err := os.Mkdir(dir, scratch.DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	old := time.Now().Add(-2 * scratch.ResolveStaleAge())
	if err := os.Chtimes(aged, old, old); err != nil {
		t.Fatalf("age %s: %v", aged, err)
	}

	t.Setenv(scratch.HomeEnvName, home) // the home arm finds nothing
	t.Setenv(scratch.GCIntervalEnvName, "10")
	t.Setenv(scratch.ResolveBaseEnvName, base) // the ONE base reader, pointed at the fixture
	t.Setenv(config.SandboxEnvName, t.TempDir())
	scratchGCLastTick = time.Time{} // isolate the process-wide clock

	report := captureStdout(t, func() {
		if got := scratchGCTick(workerRoleLabel, time.Now()); got != 0 {
			t.Errorf("the tick reported %d removals, want 0 — NOTHING of the suite-run family is the resident "+
				"tick's to delete (it shares the base with every suite on this host)", got)
		}
	})
	// The production report line, readable in this run's output (`-v`): the
	// evidence that the changed surface EXECUTED and said something (Rule 31).
	t.Logf("tick report: %s", strings.TrimSpace(report))

	// The DEFECT, asserted directly: the aged root is still there. This is the
	// assertion the pre-fix code fails.
	if _, err := os.Stat(aged); err != nil {
		t.Fatalf("the resident tick deleted the aged suite root %s: %v — that is the root of a suite running on "+
			"the same host, and the family is the OPERATOR's one-shot to reclaim (base_suite_runs.go)", aged, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("the resident tick deleted the fresh suite root %s: %v", fresh, err)
	}

	// REPORTED, not silently skipped (Rule 15): the tick must say the family is
	// there — both counts, including the stale+unheld one the pre-fix code used
	// to delete rather than count — and name the verb that reclaims it. A count
	// taken and then discarded is the quieter version of the same defect.
	for _, want := range []string{
		"2 suite run root(s)",
		"1 stale+unheld",
		CliVerbScratchGC,
		scratchGCBaseFlag,
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the tick's report does not mention %q:\n%s", want, report)
		}
	}

	// AND THE DELETING HALF STILL EXISTS where it belongs, on the same fixture:
	// the explicit entry point removes exactly the aged, unheld root the report
	// counted. The predicate is ONE (Rule 14), so the promise and the behaviour
	// cannot disagree.
	auto, kept, freshN, err := scratch.BaseSuiteRunReport(base, scratch.ResolveStaleAge())
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if auto != 1 || kept != 0 || freshN != 1 {
		t.Fatalf("report = (auto=%d kept=%d fresh=%d), want (1,0,1)", auto, kept, freshN)
	}
	removed, keptSwept, err := scratch.SweepBaseSuiteRuns(base, scratch.ResolveStaleAge())
	if err != nil {
		t.Fatalf("explicit sweep: %v", err)
	}
	if removed != 1 || keptSwept != 0 {
		t.Fatalf("explicit sweep = (removed=%d kept=%d), want (1,0)", removed, keptSwept)
	}
	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Errorf("the explicit arm did not collect %s: %v", aged, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the explicit arm removed the FRESH root %s: %v", fresh, err)
	}
}
