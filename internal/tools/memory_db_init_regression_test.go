package tools

// memory_db_init_regression_test.go — the regression guard for t-1788361069 (the
// full-suite connectionOpener hang), rewritten around the PROPERTY that
// regression is actually about.
//
// WHAT WAS WRONG WITH THE OLD GUARD (measured 2026-09-18, before this rework):
// it failed a fresh `openTasksDB` whenever the call took longer than a 2s
// wall-clock budget, and reported that as "per-DDL fsync regression (rollback
// journal still active?)". On this host the budget is mostly a measurement of
// the DISK: 20 fresh opens of the suite's own scratch root measured p50 1.29s,
// p90 3.47s, max 4.01s — and that root is not memory-backed (the suite's
// TestMain pins it under ~/workspace/gopi-test-scratch, which
// internal/scratch.MemoryFilesystem measures as "block-backed filesystem type
// 0xef53"), so firstOpenSyncMode correctly keeps synchronous=NORMAL and every
// commit legitimately waits for a real device sync. 2/12 repetitions of the
// unmodified test on a pristine tip went red for that reason alone. A wall-clock
// budget cannot separate "the disk is slow" from "the code stopped batching its
// syncs" — the two have the same symptom — so the old assertion MISDIAGNOSED the
// host as a code defect, which is what Rule 15 forbids and what made it a coin
// flip under load.
//
// WHAT THE GUARD ASSERTS INSTEAD: the pragma state a first open leaves in force.
// The regression was introduced by committing the schema build under the rollback
// journal with synchronous=FULL — each of the 40+ DDL statements its own durable
// sync (~155ms/DDL measured when it last regressed, and one stalled fsync could
// block for minutes, freezing every later sql.Open). "WAL in force, and the
// build's own connection durable" is a fact about the CODE; a slow device never
// changes it. Both arms are measured on the real production path:
//
//   - journal_mode, read back off the ledger the first open produced (it is a
//     persistent header property, so it is readable afterwards — and it is also
//     what a concurrent second opener's handle reads);
//   - synchronous, read on the build's OWN connection. It is per-connection by
//     SQLite's design, so no later handle can report it; the guard runs the
//     production build sequence through firstOpenBuildLedger for exactly that
//     reason, and TestFirstOpenKeepsProductionDurabilityOffTheWorkingPath pins
//     that the relaxed first-open mode does not leak onto later handles.
//
// The red is PROVED, not asserted: TestFirstOpenGuardIsRedOnTheRegression
// reintroduces the pre-fix pragmas through the ledgerPragmaDDLRenderer seam and
// checks the same guard goes red and then green again (see
// firstopen_pragma_guard_test.go and devdoc/firstopen-wallclock-budget-test.md).
//
// The only instrument needed is repetition (`go test -count=N`): nothing here
// measures time, and no load is manufactured (Rule 27).

import (
	"context"
	"github.com/nick/gopi/internal/heavytest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// firstOpenGuardRepetitions is how many fresh ledgers the guard opens per run.
// It buys COVERAGE, not statistics: the property is deterministic, so one open
// proves it and several catch a regression that only shows on a path a single
// open happens to avoid (a warmer file, an adopt-an-empty-file first open).
//
// Repetition is the ONLY instrument here on purpose — Rule 27 forbids
// manufacturing load, and a wall-clock budget is what this rework removes.
const firstOpenGuardRepetitions = 3

// TestFreshDBInitIsFast — the guard for t-1788361069, kept under its original
// name (callers and the suite's own docs refer to it) but asserting the property
// the regression is about instead of elapsed time.
//
// The name's claim is now the true one: a fresh ledger's first open applies WAL
// and keeps the build's connection durable, i.e. the schema init is NOT a
// per-DDL-fsync storm. Elapsed time is deliberately NOT asserted: this guard is
// the reason a slow host can no longer red it.
func TestFreshDBInitIsFast(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < firstOpenGuardRepetitions; i++ {
		path := filepath.Join(t.TempDir(), "memory.db")
		db, err := openTasksDB(ctx, path)
		if err != nil {
			t.Fatalf("rep %d: openTasksDB: %v", i, err)
		}
		var journalMode string
		if err := db.QueryRowContext(ctx, ledgerPragmaJournalModeQuery).Scan(&journalMode); err != nil {
			_ = db.Close() //stylegate:ignore the guard's own failure is reported below; this close is best-effort
			t.Fatalf("rep %d: read %s: %v", i, ledgerPragmaJournalModeQuery, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("rep %d: close: %v", i, err)
		}
		if err := ledgerJournalModeGuardError(journalMode); err != nil {
			t.Fatalf("rep %d: %v", i, err)
		}
	}

	// The durability half, on the connection the production build applies it to.
	path := filepath.Join(t.TempDir(), "durability.db")
	mode := firstOpenSyncMode(path)
	facts := firstOpenBuildLedger(t, path, mode)
	if err := ledgerPragmaGuardError(facts, mode); err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("first-open pragma state: journal_mode=%s, synchronous=%d (build mode %s) — the schema init "+
		"batches its durability instead of paying a sync per DDL", facts.journalMode, facts.synchronous, mode)
}

// TestManyFreshDBsNoHang — the suite-order reproducer: the internal/tools suite
// creates hundreds of fresh temp DBs across its tests; under the old
// rollback-journal path each one fsync'd per DDL and a single stalled fsync froze
// the whole process (database/sql connectionOpener hang at whichever test ran
// next — historically TestImportReRunIsIdempotent).
//
// The property is asserted per ledger, not a stopwatch: every one of the 40 fresh
// inits must come out in the state that makes a per-DDL fsync impossible, so
// "promptly" is a consequence rather than an assumption. A device slow enough to
// make the 40 opens take a minute is still green here; only the pragma state can
// make it red.
func TestManyFreshDBsNoHang(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	ctx := context.Background()
	for i := 0; i < manyFreshDBsIterations; i++ {
		p := filepath.Join(t.TempDir(), "memory.db")
		db, err := openTasksDB(ctx, p)
		if err != nil {
			t.Fatalf("iter %d: openTasksDB: %v", i, err)
		}
		var journalMode string
		if err := db.QueryRowContext(ctx, ledgerPragmaJournalModeQuery).Scan(&journalMode); err != nil {
			_ = db.Close() //stylegate:ignore the loop's own failure is reported below; this close is best-effort
			t.Fatalf("iter %d: read %s: %v", i, ledgerPragmaJournalModeQuery, err)
		}
		if _, err := db.Exec(`INSERT INTO tasks (task_id, summary, payload, status, tags, source, priority, created_at, updated_at) VALUES ('t','s','p','pending','','auto',5,1,1)`); err != nil {
			_ = db.Close() //stylegate:ignore the loop's own failure is reported below; this close is best-effort
			t.Fatalf("iter %d: insert: %v", i, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("iter %d: close: %v", i, err)
		}
		if err := ledgerJournalModeGuardError(journalMode); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
}

// manyFreshDBsIterations is how many fresh ledgers TestManyFreshDBsNoHang opens.
// 40 is the count the reproducer was calibrated at (devdoc/
// suite-hang-fix-t1788361069.md): it is what the suite-order hang needed to
// surface, and keeping it means the guard still walks the same path.
const manyFreshDBsIterations = 40

// TestFirstOpenGuardDoesNotMeasureTheHost pins the rework's whole point, as an
// assertion rather than a promise: the guard's verdict is a function of the
// measured pragmas alone. The same facts must give the same answer on a quiet and
// on a loaded device — and, concretely, a ledger whose FIRST OPEN WAS SLOW must
// still be green, which is the case the old budget failed.
func TestFirstOpenGuardDoesNotMeasureTheHost(t *testing.T) {
	healthy := ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncNormal}
	if err := ledgerPragmaGuardError(healthy, ledgerSyncMode); err != nil {
		t.Fatalf("the healthy pragma state failed the guard: %v", err)
	}
	// Demonstrate the separation on a real ledger: measure the open's wall time
	// and assert that it does NOT participate in the verdict. The time is logged
	// (evidence a reader can compare against the removal of the 2s budget) and
	// deliberately not asserted on.
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "evidence.db")
	start := time.Now()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	elapsed := time.Since(start)
	var journalMode string
	if err := db.QueryRowContext(ctx, ledgerPragmaJournalModeQuery).Scan(&journalMode); err != nil {
		_ = db.Close() //stylegate:ignore the guard's own failure is reported below; this close is best-effort
		t.Fatalf("read %s: %v", ledgerPragmaJournalModeQuery, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := ledgerJournalModeGuardError(journalMode); err != nil {
		t.Fatalf("%v", err)
	}
	mem, fsName, fsErr := scratch.MemoryFilesystem(filepath.Dir(path))
	t.Logf("fresh open took %v on %s (memory-backed=%v, measured %v) with journal_mode=%s — the verdict "+
		"above does not read the duration, which is why a slow device cannot red this guard",
		elapsed, fsName, mem, fsErr, journalMode)
}
