package main

// chat_prefetch_dsn_rig_test.go — the lock rigs' ledger is built in the mode
// they MEASURE, from the FIRST connection, so the fixture never needs a moment
// of exclusive access to the file.
//
// WHY THIS FILE EXISTS (measured 2026-10-15, task 01a13db4-b931-7230-9da3-83566f5b06dc).
// Five rigs in this suite hold the ledger EXCLUSIVE and assert what a read-only
// reader does while it is held. WAL makes that question unanswerable (a reader
// takes a consistent snapshot and is never refused), so each rig used to buy the
// rollback journal back with a post-hoc flip on its own holder:
//
//	holder, _ := sql.Open("sqlite", tools.LedgerDSN(path))   // WAL
//	holder.Exec(tools.LedgerRollbackJournalPragma())         // PRAGMA journal_mode = DELETE
//
// That flip needs EXCLUSIVE access to the file, and the seed is still in the
// driver's connection pool after seedPrefetchLedger returns. Measured on this
// host, the refusal is not rare and it is not about load:
//
//	A. the exact rig sequence, 300 iterations, no other handle ever opened:
//	   0/300 refusals.
//	B. the same flip with ONE sibling connection merely OPEN on the same file
//	   (a pooled, idle `sql.DB`, nothing in flight):
//	   `database is locked (5) (SQLITE_BUSY)` after 444µs.
//	C. the same flip with that sibling mid-read: refused after 15µs.
//
// So the flip is a race against a connection the fixture does not control —
// which is why it surfaced only in the full-package run (where the pool, the
// scheduler and the file's own caching differ) and never under `-run … -count=3`.
// The fix is not a retry and not a lock: it removes the NEED for exclusivity.
// A file whose FIRST connection asks for the rollback journal in its DSN is
// CREATED in rollback mode (measured: `PRAGMA journal_mode` → "delete" on the
// fresh file), so nothing ever has to flip it:
//
//	tools.LedgerRollbackJournalDSN(path)   // same header, one connection
//
// The fixture's OTHER half keeps production's contract: the path still comes
// from testutil.NewTestDBPath and the opener still goes through a tools DSN
// helper, so the busy timeout, `synchronous(NORMAL)` and the single pooled
// connection are exactly what they were.
//
// Why the mode is chosen by the RIG rather than by the seed: the same seed feeds
// rigs that measure WAL facts (TestPrefetchSessionHandlePaysNoDial reads the
// warm handle's journal_mode back as the connection's state). Giving the seed
// itself a rollback journal would change what those tests observe. The choice
// belongs to the rig that has the question.
import (
	"database/sql"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// openPrefetchLockRigLedger seeds a distilled-qa ledger in the rollback journal
// — the journal mode the lock rigs measure — and returns its path.
//
// The file is created BY tools.LedgerRollbackJournalDSN, which is what makes the
// mode a property of the file from its first byte instead of a post-hoc flip
// that needs exclusive access to a file a sibling may still hold.
func openPrefetchLockRigLedger(t *testing.T) string {
	t.Helper()
	return seedPrefetchLedgerThrough(t, func(path string) (*sql.DB, error) {
		return sql.Open(prefetchDriverName, tools.LedgerRollbackJournalDSN(path))
	})
}

// seedTurnLedgerInRollbackJournal is the same fixture for the turn rig, whose
// path the executor reads out of GOPI_MEMORY_DB rather than taking as a return
// value.
func seedTurnLedgerInRollbackJournal(t *testing.T) string {
	t.Helper()
	return seedTurnLedgerThrough(t, func(path string) (*sql.DB, error) {
		return sql.Open(prefetchDriverName, tools.LedgerRollbackJournalDSN(path))
	})
}

// assertLockRigLedgerIsInRollbackJournal reads the mode back off the file's own
// header through a FRESH connection — so the assertion is about the file, not
// about the handle that wrote it. A rig whose ledger turns out to be in WAL
// again measures nothing (its reader would never be refused), and that is what
// this guard makes loud instead of vacuous.
func assertLockRigLedgerIsInRollbackJournal(t *testing.T, path string) {
	t.Helper()
	// The readback opener asks for NO journal mode: tools.LedgerDSN declares
	// journal_mode(WAL), and a connection that declares it puts the FILE back in
	// WAL — which would make this guard the very defect it exists to catch.
	db, err := sql.Open(prefetchDriverName, tools.LedgerReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("open the ledger to read its journal mode: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("close the journal-mode reader: %v", cerr)
		}
	}()
	db.SetMaxOpenConns(1)
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode back: %v", err)
	}
	// SQLite answers the journal-mode readback in LOWER case ("delete"), which is
	// how the mode is spelled in the file header it reports — compared
	// case-insensitively so the guard reads the MODE and not its casing.
	if !strings.EqualFold(mode, tools.LedgerRollbackJournalMode()) {
		t.Fatalf("the rig's ledger is in journal_mode %q, want %q: the reader this rig measures "+
			"would not be refused under WAL, so the rig would pass without measuring anything",
			mode, tools.LedgerRollbackJournalMode())
	}
}

// lockRigFlipTries measures how often the OLD shape (a post-hoc
// `PRAGMA journal_mode = DELETE` on the holder) is refused while the fixture's
// own seed connection is still pooled. It is not a retry and it is not a repair:
// it is the red arm of the regression below, and it exists so the defect stays
// reproducible from this suite instead of living only in a task report.
const lockRigFlipTries = 32

// TestPrefetchLockRigLedgerNeedsNoExclusiveFlip is the minimal regression for the
// load-dependent red this rig was filed on. Two arms, one held question:
//
//  1. RED (the defect, reproduced): a ledger seeded on production's contract and
//     then FLIPPED by the holder — with the seed's own pooled connection still
//     attached — is refused with SQLITE_BUSY, at least once in lockRigFlipTries.
//     If this arm ever goes quiet, the flip has become safe again and the
//     assertion below should be re-derived rather than kept;
//  2. GREEN (the fix): a ledger CREATED by tools.LedgerRollbackJournalDSN reads
//     back in the rollback journal, and the holder can take BEGIN EXCLUSIVE with
//     that same sibling attached — no exclusive moment was ever needed, so there
//     is no race left to lose.
//
// The sibling is a pooled, IDLE `sql.DB`: nothing is in flight on it, which is
// the measured minimal condition (one open connection is enough to refuse the
// flip), and it is also the honest shape — the seed's connection is idle in the
// pool at exactly this moment in a real rig.
func TestPrefetchLockRigLedgerNeedsNoExclusiveFlip(t *testing.T) {
	// RED ARM — the old shape, on the same file the old rigs used.
	flipPath := seedPrefetchLedger(t)
	sib, err := sql.Open(prefetchDriverName, tools.LedgerDSN(flipPath))
	if err != nil {
		t.Fatalf("open the sibling reader: %v", err)
	}
	defer func() {
		if cerr := sib.Close(); cerr != nil {
			t.Logf("close the sibling reader: %v", cerr)
		}
	}()
	sib.SetMaxOpenConns(1)
	var rows int
	if err := sib.QueryRow("SELECT count(*) FROM distilled_qa").Scan(&rows); err != nil {
		t.Fatalf("the sibling's own read: %v", err)
	}
	holder, err := sql.Open(prefetchDriverName, tools.LedgerDSN(flipPath))
	if err != nil {
		t.Fatalf("open the flip holder: %v", err)
	}
	defer func() {
		if cerr := holder.Close(); cerr != nil {
			t.Logf("close the flip holder: %v", cerr)
		}
	}()
	holder.SetMaxOpenConns(1)
	refusals := 0
	var lastErr error
	for i := 0; i < lockRigFlipTries; i++ {
		if _, err := holder.Exec(tools.LedgerRollbackJournalPragma()); err != nil {
			refusals++
			lastErr = err
		}
	}
	if refusals == 0 {
		t.Errorf("a post-hoc journal_mode flip was never refused in %d tries with one idle sibling "+
			"connection attached — the condition this rig's fix rests on no longer holds, so "+
			"re-derive the mechanism before deleting this arm", lockRigFlipTries)
	}
	t.Logf("red arm: the post-hoc flip was refused %d/%d times with the seed's connection still attached (last: %v)",
		refusals, lockRigFlipTries, lastErr)

	// GREEN ARM — the shape the rigs use now.
	dsnPath := openPrefetchLockRigLedger(t)
	sib2, err := sql.Open(prefetchDriverName, tools.LedgerDSN(dsnPath))
	if err != nil {
		t.Fatalf("open the second sibling reader: %v", err)
	}
	defer func() {
		if cerr := sib2.Close(); cerr != nil {
			t.Logf("close the second sibling reader: %v", cerr)
		}
	}()
	sib2.SetMaxOpenConns(1)
	if err := sib2.QueryRow("SELECT count(*) FROM distilled_qa").Scan(&rows); err != nil {
		t.Fatalf("the second sibling's own read: %v", err)
	}
	holder2, err := sql.Open(prefetchDriverName, tools.LedgerDSN(dsnPath))
	if err != nil {
		t.Fatalf("open the second holder: %v", err)
	}
	defer func() {
		if cerr := holder2.Close(); cerr != nil {
			t.Logf("close the second holder: %v", cerr)
		}
	}()
	holder2.SetMaxOpenConns(1)
	if _, err := holder2.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("a ledger CREATED in the rollback journal could not be locked EXCLUSIVE while its "+
			"sibling was attached: %v — the rig has no exclusive moment to lose, so this must hold", err)
	}
	if _, err := holder2.Exec("ROLLBACK"); err != nil {
		t.Fatalf("rollback the fixture lock: %v", err)
	}
}
