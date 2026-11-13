package tools

// firstopen_pragma_guard_test.go — the guard's own regression tests, in the
// shape Rule 5 requires: the red is PROVED by reintroducing the guarded
// regression, not by asserting a time.
//
// Three claims:
//
//  1. the guard's verdict is pure and correct over a FACT table (a slow host is
//     not a red; the t-1788361069 pragma state is);
//  2. the guard reds on the PRODUCTION PATH when the regression is reintroduced
//     (the seam in firstopen_pragma_guard.go) and is green again with it removed
//     — the red-before/green-after proof, and it says WHICH pragma regressed;
//  3. the seam's production arm renders exactly the statements production used
//     before the seam existed (so the seam cannot silently BE the production
//     path's behaviour).
//
// Nothing here touches a device or a clock: repetition (`go test -count=N`) is
// the only instrument this file needs (Rule 27).

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// TestFirstOpenGuardDecisionTable pins claim 1: the verdict reads the measured
// facts, and the level it accepts is the one the mode NAMES — no more, no less
// (so neither a re-inlined duplicate FULL nor a relaxed memory-path level can
// slip through).
func TestFirstOpenGuardDecisionTable(t *testing.T) {
	cases := []struct {
		name    string
		facts   ledgerPragmaFacts
		mode    string
		wantErr bool
		why     string
	}{
		{
			name:  "wal-and-normal-is-the-expected-state",
			facts: ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncNormal},
			mode:  ledgerSyncMode,
			why:   "a device ledger: WAL, NORMAL — the WAL commit sync in force, the duplicate FULL sync gone",
		},
		{
			name:  "lower-case-wal-is-the-same-mode",
			facts: ledgerPragmaFacts{journalMode: strings.ToLower(ledgerJournalModeWAL), synchronous: firstOpenSyncNormal},
			mode:  ledgerSyncMode,
			why:   "SQLite renders journal_mode lower case; a case mismatch must not be read as a regression",
		},
		{
			name:  "wal-off-on-a-memory-ledger-is-not-a-regression",
			facts: ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncOff},
			mode:  ledgerSyncModeWorkingFS,
			why:   "the build hands OFF to a ledger that is PROVABLY on a memory filesystem, where there is no device to be durable to",
		},
		{
			name:    "rollback-journal-is-the-regression",
			facts:   ledgerPragmaFacts{journalMode: ledgerJournalModeDelete, synchronous: firstOpenSyncFull},
			mode:    ledgerSyncMode,
			wantErr: true,
			why:     "delete means every schema DDL commit pays its own fsync — the exact t-1788361069 signature",
		},
		{
			name:    "duplicate-full-under-wal-is-a-regression-too",
			facts:   ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncFull},
			mode:    ledgerSyncMode,
			wantErr: true,
			why:     "WAL + FULL is the per-commit duplicate sync the fix removes; a re-inlined synchronous=FULL must not pass",
		},
		{
			name:    "memory-ledger-that-ended-up-full-did-not-apply-its-pragma",
			facts:   ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncFull},
			mode:    ledgerSyncModeWorkingFS,
			wantErr: true,
			why:     "asked for OFF, measured FULL: the pragma never reached the connection, so the cheap path is paying for a device it does not have",
		},
		{
			name:    "device-ledger-that-ended-up-off-lost-its-commit-sync",
			facts:   ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncOff},
			mode:    ledgerSyncMode,
			wantErr: true,
			why:     "a durable ledger must not reach the memory ledger's OFF level",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ledgerPragmaGuardError(tc.facts, tc.mode)
			if tc.wantErr && err == nil {
				t.Fatalf("ledgerPragmaGuardError(%+v, %q) = nil, want an error — %s", tc.facts, tc.mode, tc.why)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ledgerPragmaGuardError(%+v, %q) = %v, want nil — %s", tc.facts, tc.mode, err, tc.why)
			}
		})
	}
}

// TestFirstOpenGuardNamesThePragma pins the failure message's job: it must say
// WHICH pragma is wrong, and it must not blame the host.
func TestFirstOpenGuardNamesThePragma(t *testing.T) {
	journalErr := ledgerPragmaGuardError(
		ledgerPragmaFacts{journalMode: ledgerJournalModeDelete, synchronous: firstOpenSyncFull}, ledgerSyncMode)
	if journalErr == nil || !strings.Contains(strings.ToLower(journalErr.Error()), strings.ToLower(ledgerJournalModeDelete)) {
		t.Fatalf("the rollback-journal failure %v does not name %q — the reader cannot tell which pragma regressed",
			journalErr, ledgerJournalModeDelete)
	}
	levelErr := ledgerPragmaGuardError(
		ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncFull}, ledgerSyncMode)
	if levelErr == nil || !strings.Contains(levelErr.Error(), ledgerSyncMode) {
		t.Fatalf("the duplicate-FULL failure %v does not name the mode %q it wanted", levelErr, ledgerSyncMode)
	}
	// A slow host cannot produce a failure here: the verdict is a function of the
	// measured facts alone, so the same facts give the same answer on a quiet and
	// on a loaded device.
	if err := ledgerPragmaGuardError(
		ledgerPragmaFacts{journalMode: ledgerJournalModeWAL, synchronous: firstOpenSyncNormal}, ledgerSyncMode); err != nil {
		t.Fatalf("the healthy state failed the guard: %v — a wall-clock sensitivity has crept into a "+
			"pragma verdict", err)
	}
}

// TestFirstOpenGuardIsRedOnTheRegression pins claims 2 and 3 with the A/B the
// acceptance requires: the SAME guard, on the SAME host, red when the pre-fix
// pragma state is reintroduced on the production path and green without it.
//
// The regression is reintroduced through the seam, and the state it reproduces is
// the one t-1788361069 was about — rollback journal + synchronous=FULL, i.e. a
// durable sync per DDL commit.
func TestFirstOpenGuardIsRedOnTheRegression(t *testing.T) {
	// Green first: the production path as shipped.
	if err := firstOpenFreshLedgerGuardError(t); err != nil {
		t.Fatalf("the unregressed production path failed the guard: %v — the red proof below would be "+
			"unfalsifiable", err)
	}

	// Red: the same path with the pre-fix pragmas.
	ledgerPragmaDDLRenderer = firstOpenGuardRegressionStmts
	defer func() { ledgerPragmaDDLRenderer = nil }()
	err := firstOpenFreshLedgerGuardError(t)
	if err == nil {
		t.Fatalf("the guard is GREEN with the pre-fix pragmas %v reintroduced — it does not actually guard "+
			"the mechanism it claims to", firstOpenGuardRegressionStmts())
	}
	if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(ledgerJournalModeDelete)) {
		t.Errorf("the red names %v, but the regression it must identify is journal_mode=%s", err, ledgerJournalModeDelete)
	}
	t.Logf("red-before proof: %v", err)

	// Green again: the seam removed, the production path restored — so the red
	// above was the regression and not the seam's mere presence.
	ledgerPragmaDDLRenderer = nil
	if err := firstOpenFreshLedgerGuardError(t); err != nil {
		t.Fatalf("the guard stayed red after the regression was reverted: %v", err)
	}
}

// firstOpenGuardRegressionStmts is the pre-fix pragma sequence the red proof
// arms the seam with: the rollback journal and a durable synchronous level —
// the state every schema DDL commit pays an fsync in (t-1788361069). It is the
// seam's ONLY use of the two named values, so what the red proves and what the
// constants document cannot drift apart.
func firstOpenGuardRegressionStmts() []string {
	return []string{
		ledgerPragmaJournalModePrefix + ledgerJournalModeDelete,
		ledgerPragmaSynchronousPrefix + ledgerSyncModeRegressed,
	}
}

// firstOpenFreshLedgerGuardError opens a fresh ledger through the PRODUCTION
// entry point and returns the guard's verdict on the state it left in force —
// the journal mode read off the ledger itself (arm 1), plus the level measured on
// the connection the build ran on (arm 2). Both halves of the property, on the
// real path.
func firstOpenFreshLedgerGuardError(t *testing.T) error {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	var journalMode string
	if err := db.QueryRowContext(ctx, ledgerPragmaJournalModeQuery).Scan(&journalMode); err != nil {
		t.Fatalf("read %s off the ledger the first open produced: %v", ledgerPragmaJournalModeQuery, err)
	}
	if err := ledgerJournalModeGuardError(journalMode); err != nil {
		return err
	}
	return firstOpenSyncLevelGuardError(t, path)
}

// firstOpenSyncLevelGuardError runs the SAME schema build the production entry
// point runs, on a handle the caller holds, so the synchronous LEVEL can be
// measured on the connection it was applied to. It is the second half of the
// guard: the level is per-connection, so reading it later off a fresh handle
// would only ever report SQLite's default.
func firstOpenSyncLevelGuardError(t *testing.T, path string) error {
	t.Helper()
	mode := firstOpenSyncMode(path)
	facts := firstOpenBuildLedger(t, filepath.Join(filepath.Dir(path), "level-probe.db"), mode)
	return ledgerSyncLevelGuardError(mode, facts.synchronous)
}

// firstOpenBuildLedger runs the production schema-build sequence (the pragma
// statements from ledgerPragmaStmts, then the three schema phases) against a
// fresh ledger and returns the pragma state left in force ON THAT CONNECTION —
// the level half of the property, measurable only while the build's own handle
// is alive.
func firstOpenBuildLedger(t *testing.T, path, syncMode string) ledgerPragmaFacts {
	t.Helper()
	ctx := context.Background()
	db, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if err := enableWALWithSync(ctx, db, syncMode); err != nil {
		_ = db.Close() //stylegate:ignore the build failure is reported below; this close is best-effort
		t.Fatalf("enableWALWithSync(%s): %v", syncMode, err)
	}
	for _, phase := range []struct {
		name string
		run  func() error
	}{
		{name: "ensureSchema", run: func() error { return ensureSchema(ctx, db) }},
		{name: "ensureFeedbackSchema", run: func() error { return ensureFeedbackSchema(ctx, db) }},
		{name: "ensureTasksSchema", run: func() error { return ensureTasksSchema(ctx, db) }},
	} {
		if err := phase.run(); err != nil {
			_ = db.Close() //stylegate:ignore the build failure is reported below; this close is best-effort
			t.Fatalf("%s: %v", phase.name, err)
		}
	}
	facts, err := ledgerReadPragmaFacts(ctx, db)
	if err != nil {
		_ = db.Close() //stylegate:ignore the read failure is reported below; this close is best-effort
		t.Fatalf("read the build connection's pragmas: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	return facts
}

// TestFirstOpenGuardSeamRendersProductionStmts pins claim 3: with the seam
// unarmed, the statement list is exactly the WAL + requested-synchronous pair
// production has always issued. A seam that changed the production path's own
// behaviour would make claim 2 vacuous (the red could be the seam rather than the
// regression).
func TestFirstOpenGuardSeamRendersProductionStmts(t *testing.T) {
	if ledgerPragmaDDLRenderer != nil {
		t.Fatal("the seam is armed by default — the production path must never set it")
	}
	for _, mode := range []string{ledgerSyncMode, ledgerSyncModeWorkingFS} {
		got := ledgerPragmaStmts(mode)
		want := []string{ledgerJournalModeStmt, ledgerPragmaSynchronousPrefix + mode}
		if len(got) != len(want) {
			t.Fatalf("ledgerPragmaStmts(%q) = %v, want %v", mode, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("ledgerPragmaStmts(%q)[%d] = %q, want %q", mode, i, got[i], want[i])
			}
		}
	}
	// The seam, once armed, replaces the whole sequence — otherwise the
	// regression could not be reproduced (a journal_mode-only rewrite leaves the
	// durable level in place and the build fast).
	ledgerPragmaDDLRenderer = firstOpenGuardRegressionStmts
	defer func() { ledgerPragmaDDLRenderer = nil }()
	armed := ledgerPragmaStmts(ledgerSyncMode)
	if len(armed) == 0 || !strings.Contains(armed[0], ledgerJournalModeDelete) {
		t.Fatalf("the armed seam renders %v, which does not reintroduce journal_mode=%s", armed, ledgerJournalModeDelete)
	}
}

// Keep the sql import honest: the guard's helpers take a *sql.DB.
var _ = (*sql.DB)(nil)
