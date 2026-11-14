package tools

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestLedgerDSNCarriesProductionsContract pins the single-source-of-truth
// property (Rules 1a/13/14) of LedgerDSN: the busy timeout it hands a
// non-production opener must be dbBusyTimeoutMSec — the SAME constant openDB
// applies — so retuning production retunes every fixture with it, and no call
// site can re-type the number.
//
// It also pins the two halves the fix depends on: a busy timeout (without it the
// opener fails instantly with SQLITE_BUSY instead of queueing) AND journal_mode
// WAL (production's own persistent header property; measured, a timeout alone
// still lets the fixture's WRITE lose the race, and WAL alone still has it fail
// — both are needed).
func TestLedgerDSNCarriesProductionsContract(t *testing.T) {
	const path = "/tmp/does-not-need-to-exist/memory.db"
	dsn := LedgerDSN(path)

	if !strings.HasPrefix(dsn, "file:"+path+"?") {
		t.Errorf("LedgerDSN(%q) = %q: want a file: DSN naming the exact path", path, dsn)
	}
	wantTimeout := "busy_timeout(" + strconv.Itoa(dbBusyTimeoutMSec) + ")"
	if !strings.Contains(dsn, wantTimeout) {
		t.Errorf("LedgerDSN(%q) = %q: must carry %q (the value of dbBusyTimeoutMSec, never a re-typed "+
			"literal)", path, dsn, wantTimeout)
	}
	if !strings.Contains(dsn, "journal_mode(WAL)") {
		t.Errorf("LedgerDSN(%q) = %q: must state production's WAL journal mode — without it the fixture's "+
			"write still loses the race", path, dsn)
	}
	// The sync mode is the third half of the contract and the one that decides
	// whether a fixture's COMMIT costs one fsync (FULL, the default) or none
	// (NORMAL under WAL). On a host whose fsync measures ~1s the difference is a
	// minute-long stall — measured, memory.go:595.
	wantSync := "synchronous(" + ledgerSyncMode + ")"
	if !strings.Contains(dsn, wantSync) {
		t.Errorf("LedgerDSN(%q) = %q: must carry %q — the SAME sync mode enableWAL applies to production's "+
			"handle, so a fixture's commit does not pay a full fsync on a loaded disk", path, dsn, wantSync)
	}
	// The re-typed literal is the defect this guards: if the value ever stops
	// being the constant, the two numbers drift apart silently.
	if strings.Contains(dsn, "15000") && strconv.Itoa(dbBusyTimeoutMSec) != "15000" {
		t.Errorf("LedgerDSN(%q) = %q: carries a literal 15000 while dbBusyTimeoutMSec = %d — the DSN must "+
			"be built from the constant", path, dsn, dbBusyTimeoutMSec)
	}
}

// TestLedgerMemoryDSNOpensAPrivateDatabaseThatRefusesWAL pins the exempt class
// LedgerMemoryDSN names, and it pins it with the ENGINE's verdict rather than a
// restatement of the claim: the reason an in-memory opener is outside the ledger
// DSN contract is that the contract's parts cannot hold there, and every part
// below is measured on a real handle.
//
// Three facts, each one load-bearing for the exemption in
// cmd/agent/ledger_dsn_contract_test.go:
//
//  1. the DSN names an in-memory database and NOTHING else — no path, no
//     `file:`, no pragma pretending to carry a contract it cannot;
//  2. it OPENS (a helper returning a spelling the driver refuses would turn the
//     exemption into a broken fixture);
//  3. WAL is REFUSED (SQLite answers `memory`), which is what makes "no DSN
//     spelling can carry the contract here" a measurement instead of an
//     assertion, and a SECOND handle cannot see this handle's table — the
//     privacy that makes a race with a resident daemon impossible.
//
// NOTE ON HOW THIS TEST OPENS ITS HANDLES. Both call sites pass the helper call
// INLINE rather than through a local variable, and that is deliberate: the DSN
// contract gate (cmd/agent/ledger_dsn_contract_test.go) reads the OPEN SITE, so
// `sql.Open("sqlite", dsn)` — the value arriving in a variable — is flagged
// exactly like a bare literal is. Measured: this test, written the variable way,
// reddened the gate at its own two lines. That is the instrument behaving
// correctly, not a workaround to be coded around, and it is worth knowing before
// writing the next in-memory fixture.
func TestLedgerMemoryDSNOpensAPrivateDatabaseThatRefusesWAL(t *testing.T) {
	const memoryJournalMode = "memory" // what SQLite answers when WAL is impossible
	dsn := LedgerMemoryDSN()
	if dsn != ledgerMemoryDSN {
		t.Errorf("LedgerMemoryDSN() = %q, want the ONE definition %q (Rule 2: the value is stated in "+
			"exactly one place)", dsn, ledgerMemoryDSN)
	}
	if strings.Contains(dsn, "file:") || strings.Contains(dsn, "?") {
		t.Errorf("LedgerMemoryDSN() = %q: an in-memory DSN carries no path and no pragma query — a DSN "+
			"that states contract clauses it cannot honour is worse than one that states none", dsn)
	}
	// db-scaffold-ok: this test resolves no database path at all — the DSN is the
	// package's own in-memory constant, so the operator's append-only ledger is
	// unreachable from it (Rule 32's concern is a helper resolving its OWN path).
	db, err := sql.Open("sqlite", LedgerMemoryDSN())
	if err != nil {
		t.Fatalf("sql.Open(sqlite, %q): %v — the exempt class must still open", dsn, err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE probe(x)`); err != nil {
		t.Fatalf("create a table on an in-memory database: %v", err)
	}
	var mode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode=WAL`).Scan(&mode); err != nil {
		t.Fatalf("ask the engine for WAL: %v", err)
	}
	if mode != memoryJournalMode {
		t.Errorf("PRAGMA journal_mode=WAL on %q answered %q, want %q — the exempt class is exempt BECAUSE "+
			"the engine refuses WAL (and therefore every file-DSN spelling of the contract) for an "+
			"in-memory database; if this ever changes, revisit the exemption, which cites exactly this "+
			"measurement", dsn, mode, memoryJournalMode)
	}
	other, err := sql.Open("sqlite", LedgerMemoryDSN())
	if err != nil {
		t.Fatalf("second in-memory handle: %v", err)
	}
	defer other.Close()
	var count int
	if err := other.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 'probe'`).Scan(&count); err != nil {
		t.Fatalf("inspect the second handle: %v", err)
	}
	if count != 0 {
		t.Errorf("a second handle sees %d `probe` table(s): an in-memory database opened this way is "+
			"PRIVATE to its handle, which is the property that makes it unable to race any other "+
			"process's ledger — the whole reason the contract does not describe it", count)
	}
}

// TestLedgerRollbackJournalPragmaIsWhatMakesAHolderRefuse is the guard for
// LedgerRollbackJournalPragma, and it exists because that pragma is what stands
// between two cmd/agent rigs and a SILENT LOSS OF THEIR SUBJECT.
//
// THE MECHANISM IT PINS (measured here, not argued): the two rigs
// (TestPrefetchBusyIsRetriedWithinTheSameBudget, TestPrefetchBusyGiveUpIsReportedAsBusy)
// hold the ledger with `BEGIN EXCLUSIVE` and then assert that a read-only
// prefetch lookup is REFUSED with SQLITE_BUSY and gives up inside its budget.
// While the ledger is in WAL that assertion is unobservable: a read-only reader
// never contends with a writer — it reads a consistent snapshot — so the lookup
// returns `hits` in ~0ms and the rig fails on its own expectation. Once the
// fixture's seed took production's DSN contract (Rule 14: one helper, and that
// helper carries journal_mode(WAL)) the rigs silently stopped testing the
// give-up they are named for. Their holder asks for the rollback journal back
// through this pragma, and THIS test is the proof that the pragma is
// load-bearing rather than decorative.
//
// Rule 27: no load is generated. The holder is a STATE (one EXCLUSIVE lock), and
// the reader's wait is bounded by its own busy timeout — nothing saturates.
func TestLedgerRollbackJournalPragmaIsWhatMakesAHolderRefuse(t *testing.T) {
	path := testutil.NewTestDBPath(t)

	// The ledger as production leaves it: WAL, through the ONE write-capable DSN.
	seed, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatalf("open the seed: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create the probe table: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO probe (v) VALUES ('x')`); err != nil {
		t.Fatalf("seed the probe row: %v", err)
	}
	// Closed BEFORE any later handle tries to flip the journal mode: a
	// journal_mode change needs exclusive access to the file, so an open handle
	// of our own is enough to refuse it. (Measured here — leaving this open made
	// the flip below fail with SQLITE_BUSY, which is the same mechanism the two
	// cmd/agent rigs had to respect.)
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seed: %v", err)
	}

	const busyTimeoutMS = 500
	readUnderHold := func(t *testing.T, holder *sql.DB) time.Duration {
		t.Helper()
		reader, err := sql.Open("sqlite", LedgerReadOnlyDSNIn(path, busyTimeoutMS))
		if err != nil {
			t.Fatalf("open the read-only reader: %v", err)
		}
		defer reader.Close()
		reader.SetMaxOpenConns(1)

		started := time.Now()
		var v string
		err = reader.QueryRow(`SELECT v FROM probe LIMIT 1`).Scan(&v)
		elapsed := time.Since(started)
		if strings.EqualFold(ledgerJournalModeOf(t, holder), ledgerRollbackJournalMode) {
			// The rollback-journal arm: the holder's lock must REFUSE the reader,
			// and the refusal must be the bounded one (the DSN's busy timeout),
			// never a hang.
			if !LedgerBusyErr(err) {
				t.Fatalf("under %s the holder's EXCLUSIVE lock must refuse the reader, got "+
					"err=%v after %s — the rig that relies on this pragma would be measuring "+
					"nothing", ledgerRollbackJournalMode, err, elapsed)
			}
			return elapsed
		}
		// The WAL arm: the reader is SERVED, and that is the fact that makes the
		// pragma necessary.
		if err != nil {
			t.Fatalf("under WAL a read-only reader must not contend with a writer, got %v", err)
		}
		return elapsed
	}

	// ARM 1 — production's mode: WAL. The reader is served, i.e. the rig's
	// assertion could not have been observed here.
	wALHolder, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatalf("open the WAL holder: %v", err)
	}
	defer wALHolder.Close()
	wALHolder.SetMaxOpenConns(1)
	if _, err := wALHolder.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("BEGIN EXCLUSIVE (WAL arm): %v", err)
	}
	var got string
	if err := wALHolder.QueryRow(`SELECT v FROM probe LIMIT 1`).Scan(&got); err != nil {
		t.Fatalf("the WAL holder cannot see its own ledger: %v", err)
	}
	if mode := ledgerJournalModeOf(t, wALHolder); !strings.EqualFold(mode, ledgerWALJournalMode) {
		t.Fatalf("the WAL arm is not in WAL: journal_mode=%q — the two arms would be the same "+
			"measurement and this test would prove nothing", mode)
	}
	readUnderHold(t, wALHolder)

	// ARM 2 — the pragma: rollback journal. Same holder, same lock, and now the
	// reader IS refused. The only difference between the arms is the pragma.
	if _, err := wALHolder.Exec("ROLLBACK"); err != nil {
		t.Fatalf("release the WAL-arm lock: %v", err)
	}
	if _, err := wALHolder.Exec(LedgerRollbackJournalPragma()); err != nil {
		t.Fatalf("%s on a fresh handle (no reader attached yet) must be accepted: %v — the "+
			"journal flip needs exclusive access, which is why the rigs flip it BEFORE warming "+
			"a reader", LedgerRollbackJournalPragma(), err)
	}
	if _, err := wALHolder.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("BEGIN EXCLUSIVE (rollback arm): %v", err)
	}
	readUnderHold(t, wALHolder)
}

// ledgerJournalModeOf reads the journal mode off an ALREADY-OPEN handle, which
// is the only honest reading: journal_mode is a property of the file as the
// handle currently sees it.
func ledgerJournalModeOf(t *testing.T, db *sql.DB) string {
	t.Helper()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	return mode
}
