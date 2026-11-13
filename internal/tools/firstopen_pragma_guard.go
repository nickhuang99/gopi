package tools

// firstopen_pragma_guard.go — the PROPERTY the first-open regression guard
// asserts, and the seam that lets the guarded regression be reintroduced so the
// guard's red is proven (Rule 5: a fix ships a reproducer).
//
// WHY A PROPERTY AND NOT A WALL-CLOCK BUDGET (the defect this file replaces):
// TestFreshDBInitIsFast used to fail whenever a fresh open took longer than 2s,
// and call that "per-DDL fsync regression (rollback journal still active?)".
// Measured 2026-09-18 on this host: 20 fresh in-process opens of the suite's own
// scratch root — which is NOT memory-backed (internal/scratch.MemoryFilesystem
// reports "block-backed filesystem type 0xef53", so firstOpenSyncMode correctly
// keeps synchronous=NORMAL and each commit legitimately pays a real device
// sync) — took p50 1.293s, p90 3.472s, max 4.010s. The old budget was therefore
// comparing a HOST measurement against a constant, and a slow device read as a
// code defect: the exact misdiagnosis Rule 15 forbids. The t-1788361069
// regression it meant to guard is a statement about the PRAGMAS IN FORCE at
// first open (WAL, not the rollback journal; synchronous=NORMAL, not FULL), and
// that is what ledgerPragmaGuardError now reads off the opened ledger — a fact
// about the code, not about the disk.
//
// THE SEAM (ledgerPragmaDDLRenderer): proving the guard fails requires the
// regression to actually be reintroduced in the production path, and the only
// way to do that without committing the regression is a seam the test arms.
// Production never sets it (nil), so its cost is one nil check per pragma
// statement; the same shape as the package's other test seams
// (schemaBatchProbeHook, syncSSHExchangeHook).

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// The pragma statements and the values they are rendered from, named once
// (Rule 1b): the production path, the guard's read-back and the regression arm
// all have to agree on these spellings, and a bare literal at any of those sites
// would silently decouple them.
const (
	// ledgerPragmaJournalModePrefix renders a journal_mode statement from one of
	// the ledgerJournalMode* values.
	ledgerPragmaJournalModePrefix = "PRAGMA journal_mode="
	// ledgerJournalModeWAL is the journal mode every gopi-owned ledger runs in:
	// under the rollback journal each schema DDL commit fsyncs (t-1788361069,
	// the connectionOpener hang), while WAL turns the DDL storm into cheap
	// commits (see enableWALWithSync).
	ledgerJournalModeWAL = "WAL"
	// ledgerJournalModeDelete is the rollback journal — SQLite's default, and
	// the mode a first open regresses TO. Named because the regression arm must
	// reproduce the pre-fix state from the same constant the guard reads back.
	ledgerJournalModeDelete = "DELETE"
	// ledgerJournalModeStmt is the statement a first open issues first.
	ledgerJournalModeStmt = ledgerPragmaJournalModePrefix + ledgerJournalModeWAL
	// ledgerPragmaSynchronousPrefix renders the synchronous= statement from a
	// mode in this package's closed set (ledgerSyncMode /
	// ledgerSyncModeWorkingFS).
	ledgerPragmaSynchronousPrefix = "PRAGMA synchronous="
	// ledgerPragmaJournalModeQuery and ledgerPragmaSynchronousQuery read back the
	// state in force. They are the MEASUREMENT the guard asserts on — not a
	// restatement of what was requested.
	ledgerPragmaJournalModeQuery = "PRAGMA journal_mode"
	ledgerPragmaSynchronousQuery = "PRAGMA synchronous"
	// firstOpenSyncOff / firstOpenSyncNormal / firstOpenSyncFull are SQLite's
	// `PRAGMA synchronous` levels as it reports them (0=OFF, 1=NORMAL, 2=FULL,
	// 3=EXTRA — the latter not used by this package). Named (Rule 1a) because the
	// guard's level comparison is meaningless without agreeing on which integer is
	// which level, and because firstOpenSyncFull is ALSO the level a handle
	// inherits when the pragma was never applied — which is why the journal mode
	// must be asserted beside it.
	firstOpenSyncOff    = 0
	firstOpenSyncNormal = 1
	firstOpenSyncFull   = 2
)

// ledgerPragmaDDLRenderer, when non-nil, replaces the pragma statements a first
// open applies. It exists so a regression test can reintroduce the pre-fix
// pragma state (rollback journal, durable synchronous) on the PRODUCTION path
// and prove the guard goes red; the production path never sets it.
var ledgerPragmaDDLRenderer func() []string

// ledgerPragmaStmts is the ordered pragma statements a first open applies to its
// own connection for the requested synchronous mode.
//
// It is a function (not two inline Exec calls at the call site) because the
// regression arm has to be able to replace the whole sequence: a seam that
// replaced only one of the two statements could not reproduce the regression,
// and the guard's red would rest on a state production cannot actually reach.
func ledgerPragmaStmts(syncMode string) []string {
	if ledgerPragmaDDLRenderer != nil {
		return ledgerPragmaDDLRenderer()
	}
	return []string{ledgerJournalModeStmt, ledgerPragmaSynchronousPrefix + syncMode}
}

// ledgerPragmaFacts is the pragma state measured on an opened ledger: what
// SQLite reports, longhand — never what the code asked for.
type ledgerPragmaFacts struct {
	// journalMode is `PRAGMA journal_mode` (SQLite renders it lower case).
	journalMode string
	// synchronous is `PRAGMA synchronous` as a level: 0=OFF, 1=NORMAL, 2=FULL,
	// 3=EXTRA.
	synchronous int
}

// ledgerReadPragmaFacts reads both pragmas off a live handle.
func ledgerReadPragmaFacts(ctx context.Context, db *sql.DB) (ledgerPragmaFacts, error) {
	var f ledgerPragmaFacts
	if err := db.QueryRowContext(ctx, ledgerPragmaJournalModeQuery).Scan(&f.journalMode); err != nil {
		return ledgerPragmaFacts{}, fmt.Errorf("read %s: %w", ledgerPragmaJournalModeQuery, err)
	}
	if err := db.QueryRowContext(ctx, ledgerPragmaSynchronousQuery).Scan(&f.synchronous); err != nil {
		return ledgerPragmaFacts{}, fmt.Errorf("read %s: %w", ledgerPragmaSynchronousQuery, err)
	}
	return f, nil
}

// ledgerSyncModeLevel maps a synchronous= mode from this package's closed set to
// the level SQLite reports for it, and whether the mode is known at all.
//
// It is a function rather than a comparison of literals so the mode→level
// agreement has ONE definition: the guard reads the measured level against it,
// the decision table exercises it, and a future change to ledgerSyncMode's value
// (say to "FULL") cannot silently pass the guard while reintroducing the
// duplicate per-commit sync.
func ledgerSyncModeLevel(syncMode string) (int, bool) {
	switch syncMode {
	case ledgerSyncModeWorkingFS:
		return firstOpenSyncOff, true
	case ledgerSyncMode:
		return firstOpenSyncNormal, true
	default:
		return 0, false
	}
}

// ledgerSyncModeIsNotPerCommitSync reports whether a connection asked for
// syncMode ended up at the level that mode names — i.e. the first open no longer
// pays a durable sync per commit, which is the property the t-1788361069 fix
// establishes and this guard protects.
//
// THE LEVELS, precisely, because the whole guard rests on them: `PRAGMA
// synchronous` reports 0=OFF, 1=NORMAL, 2=FULL, 3=EXTRA.
//
//   - OFF (0) omits the sync altogether. firstOpenSyncMode (memory.go:560) hands
//     the build this level ONLY when the ledger provably lives on a memory
//     filesystem (measured by device id, never a path spelling), where there is
//     no device to be durable to — the dropped sync protected nothing.
//   - NORMAL (1) does NOT skip the commit's sync: under WAL it syncs the WAL at
//     COMMIT, i.e. at the commit boundary. That is not a durability loss — it is
//     what removes the ROLLBACK JOURNAL's extra sync — and it is the level
//     enableWAL/ledgerSyncMode apply on purpose (ledger_dsn.go:80). A WAL ledger
//     at NORMAL is the GOOD state, so an assertion demanding more than this would
//     call production's default a regression.
//   - FULL (2) is SQLite's default and the DUPLICATE this fix removes under WAL
//     (ledger_dsn.go:77-79: "under the default FULL every COMMIT fsyncs"): WAL
//     already syncs the WAL at commit, so FULL adds a further per-commit sync of
//     the WAL header/directory — the extra syscall behind the ~155ms-per-DDL
//     signature. Re-inlining it under WAL is therefore a regression this arm must
//     still catch, which is why the comparison is EQUALITY with the mode's level
//     and not a floor.
//
// Consequences: `delete` + FULL (the historical pre-fix state) reds on BOTH arms,
// and a mode/level drift (asked for OFF, measured FULL — a pragma that never
// reached the connection) reds on this one. An unknown mode is not proven cheap
// and reds too.
func ledgerSyncModeIsNotPerCommitSync(syncMode string, level int) bool {
	want, ok := ledgerSyncModeLevel(syncMode)
	if !ok {
		return false
	}
	return level == want
}

// ledgerPragmaGuardError is the guard's verdict over BOTH arms of the property —
// journal mode and durability level — so a caller that can only measure one of
// them calls the arm it can (ledgerJournalModeGuardError /
// ledgerSyncLevelGuardError). It is a pure function of the measured state: the
// assertion is unit-testable, its reds are provable without touching a device
// (Rule 5), and no failure message can name the host instead of the
// mechanism.
func ledgerPragmaGuardError(facts ledgerPragmaFacts, syncMode string) error {
	if err := ledgerJournalModeGuardError(facts.journalMode); err != nil {
		return err
	}
	return ledgerSyncLevelGuardError(syncMode, facts.synchronous)
}

// ledgerJournalModeGuardError is the journal-mode arm: the ledger must be in WAL,
// because that is what stops a schema DDL commit from being its own durable sync
// (t-1788361069 — see enableWALWithSync).
func ledgerJournalModeGuardError(journalMode string) error {
	if strings.EqualFold(journalMode, ledgerJournalModeWAL) {
		return nil
	}
	return fmt.Errorf("the first open left journal_mode=%s, want %s — the ledger's schema build is "+
		"committing under the rollback journal, so every one of its %s DDL statements can pay its own "+
		"fsync (the t-1788361069 regression; ~155ms per DDL measured when it last regressed)",
		strings.ToLower(journalMode), strings.ToLower(ledgerJournalModeWAL), schemaDDLStatementCountEstimate)
}

// ledgerSyncLevelGuardError is the level arm: measured against the mode the build
// was ASKED for, because the correct level depends on it — OFF for a ledger
// provably on a memory filesystem, at least NORMAL (the WAL commit sync) for any
// device ledger. See ledgerSyncModeIsNotPerCommitSync for why NORMAL is the
// production default here and FULL is the duplicate this fix removes.
//
// This arm deliberately says "a slow host is not this": the t-1788361069
// regression and a slow device both make a first open expensive, and only one of
// them is a code defect — which is why the guard asserts the pragma state rather
// than elapsed time.
func ledgerSyncLevelGuardError(syncMode string, level int) error {
	if ledgerSyncModeIsNotPerCommitSync(syncMode, level) {
		return nil
	}
	if syncMode == ledgerSyncModeWorkingFS {
		return fmt.Errorf("the first open chose synchronous=%s for a ledger provably on a memory "+
			"filesystem, but its connection reads level %d — the cheap path is paying for a device it does "+
			"not have (the pragma was not applied, or SQLite's FULL default leaked through)",
			ledgerSyncModeWorkingFS, level)
	}
	return fmt.Errorf("the first open's connection reads synchronous=%d, but %s (level >= %d) is what keeps "+
		"the WAL commit sync in force — an OFF/unset level here means a committed schema DDL is no longer "+
		"made durable at the commit boundary at all (the t-1788361069 durability is broken); note that a "+
		"slow host is a different matter entirely and never fails this guard",
		level, ledgerSyncMode, firstOpenSyncNormal)
}

// schemaDDLStatementCountEstimate is the "40+" in the schema build's own doc
// comments, named (Rule 1a) because the guard's failure message quantifies the
// per-statement fsync cost with it and a re-typed figure would drift from the
// explanation of the same mechanism.
const schemaDDLStatementCountEstimate = "40+"
