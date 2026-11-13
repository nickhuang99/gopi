package tools

// memory_sync_assets_batch_test.go — mechanism 2 of the memsync failure family
// (2026-09-26): the asset import used to apply a WHOLE peer export in ONE
// BEGIN IMMEDIATE, so a `gopi memory sync --peer <peer>` leg write-locked the
// PEER's ledger for the entire apply (measured ~40s on a 5209-row corpus) and
// every concurrent writer on that peer died of SQLITE_BUSY after
// dbBusyTimeoutMSec.
//
// These tests pin the BATCHED contract in memory_sync_assets.go with
// instruments that involve no load and no device pressure (Rule 27 — the probe
// only ATTEMPTS to acquire the same write lock a concurrent gopi writer would
// attempt): ① a real second connection sampling "is the peer ledger's write
// lock free right now?", so the metric is the defect's own unit — WHEN does a
// concurrent writer first get in while an import applies a corpus? ② a
// deterministic mid-apply failure, injected two independent ways (the DEBUG
// fixture seam, and the memory-recorded RAISE(ABORT) trigger recipe — memory
// #5589, devdoc/memsync-import-atomic.md), so the failure lands at an exact
// batch and an exact row with no timing dependence. ③ a real MemoryStore
// corpus whose rows genuinely INSERT (measured: a first attempt with a shared
// template collapsed into imported=7/deduped=93 — a corpus the store dedups
// measures the dedup path, not the write path the peer lock is about).

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// batchTestRows is the corpus size. It must be comfortably more than ONE batch:
// a batch is 32 rows (memoryAssetImportBatchRows) and the lock probe's sampling
// resolution is its own busy timeout, so a corpus of two batches is the minimum
// that can show "the lock was released between batches".
const batchTestRows = 100

// batchTestFailRow is the 1-based INSERT number the injected SQLite trigger
// aborts on — 40 lands inside the SECOND batch at the production batch size
// (32), so the test can tell a prefix of committed batches from a whole-patch
// rollback.
const batchTestFailRow = 40

// peerLedgerLockHeld answers the question this whole file exists to answer —
// "can a second connection take the peer ledger's write lock RIGHT NOW?" — with a
// real connection, synchronously, in microseconds: `BEGIN IMMEDIATE` (with
// busy_timeout=0, so the answer is immediate) followed by ROLLBACK on success.
// It therefore never holds the lock beyond the probe itself.
//
// It is deliberately NOT a background sampler. Two sampling designs were built
// and measured before this one, and both are recorded because each looks
// reasonable and neither can work here:
//
//	production busy_timeout (15s): SQLite's busy handler parks the probe inside
//	  ONE refused attempt for the whole apply — measured: first_ok_at = 7.1s of
//	  a 1.5s apply, a number with no relation to the lock.
//	50ms, then a paced 250µs poll at busy_timeout=0: the gap between two batches
//	  is narrower than one poll period, so a correct per-batch implementation was
//	  reported as a defect (measured: lock_free_episodes=1 for a 4-batch apply,
//	  oks=2), and an unpaced loop starved the import it was measuring (1ms/row
//	  stretched to 4-5ms/row).
//
// The synchronous probe has neither failure mode: the import is not racing it,
// it CALLS it — from the batch-boundary seam, on both sides of the commit. So
// the observation is deterministic: what a concurrent writer would see at this
// exact boundary.
func peerLedgerLockHeld(t *testing.T, dbPath string) bool {
	t.Helper()
	// The DSN is the PRODUCTION one (Rule 13/14 — never a re-typed literal), so
	// the driver and the open mode are exactly what production uses; the ONE
	// deliberate deviation is the busy handler, disabled on the handle below.
	// Without that deviation the probe parks inside a single refused attempt for
	// the whole hold (measured: first_ok_at = 7.1s of a 1.5s apply) and stops
	// measuring the lock at all. The pragma's spelling and its value live in the
	// helper (internal/tools.ledger_dsn.go), never here.
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("probe open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(ledgerNoWaitPragma()); err != nil {
		t.Fatalf("probe disable busy handler (%s): %v", ledgerNoWaitPragma(), err)
	}
	if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
		if !isSQLiteBusy(err) {
			t.Fatalf("probe BEGIN IMMEDIATE failed for a reason other than BUSY: %v", err)
		}
		return true // refused ⇒ the write lock is held by someone else
	}
	if _, err := db.Exec("ROLLBACK"); err != nil {
		t.Fatalf("probe ROLLBACK: %v", err)
	}
	return false
}

// isSQLiteBusy reports whether an acquisition was REFUSED (someone else holds
// the lock) rather than failing for a real reason — a refusal must never be
// confused with a broken probe (Rule 15). The driver surfaces SQLite's own code
// and message, so both spellings are matched.
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// batchPatchRow is the patch row at index i — the SAME spelling the patch and
// the assertions read (one definition, Rule 13).
//
// Each row carries a per-row topic and a long run of pseudo-random base36
// tokens, so no two rows share enough trigrams to reach either dedup screen:
// the corpus must genuinely INSERT, because that is the work a peer export costs.
func batchPatchRow(i int) SyncRow {
	return SyncRow{
		Topic:    fmt.Sprintf("batchtopic%03d", i),
		Question: fmt.Sprintf("batchpatch row %03d: %s %s %s %s %s", i, batchToken(i, 1), batchToken(i, 2), batchToken(i, 3), batchToken(i, 4), batchToken(i, 5)),
		Answer:   fmt.Sprintf("batchpatch answer %03d carries %s %s %s %s %s", i, batchToken(i, 6), batchToken(i, 7), batchToken(i, 8), batchToken(i, 9), batchToken(i, 10)),
	}
}

// batchToken derives the nth (index, salt) pseudo-random base36 token of a
// patch row — splitmix64, so the corpus is deterministic, dependency-free and
// reproducible (a fixture that changes between runs cannot be the basis of a
// measurement).
func batchToken(index, salt int) string {
	x := uint64(index)*0x9E3779B97F4A7C15 + uint64(salt)*0xBF58476D1CE4E5B9
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return strconv.FormatUint(x, 36)
}

// batchPatch builds a patch of n rows the target does NOT hold.
func batchPatch(n int) *SyncPatch {
	rows := make([]SyncRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, batchPatchRow(i))
	}
	return &SyncPatch{Version: syncPatchVersion, Node: "hpbatch", Rows: rows}
}

// batchTestDB creates a fresh knowledge DB holding `seeded` rows and returns its
// path. Rows are written through MemoryStore (the REAL write path), so the file
// carries the production schema (distilled_qa + FTS triggers). The env pin is
// the channel the CLI verb itself resolves (empty --db → GOPI_MEMORY_DB), so the
// tests drive the import exactly as the command does.
func batchTestDB(t *testing.T, seeded int) string {
	t.Helper()
	dbPath := t.TempDir() + "/memory.db"
	t.Setenv(memoryDBEnv, dbPath)
	for i := 0; i < seeded; i++ {
		q := fmt.Sprintf("batchseed question %03d about a distinctly seeded subject", i)
		if _, err := MemoryStore("batchseed", q, fmt.Sprintf("seed answer %d with plenty of meaningful characters", i), BrewSourceTag, nil); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	return dbPath
}

// batchPatchCount counts how many of the patch's rows the target DB holds.
func batchPatchCount(t *testing.T, dbPath string, n int) int {
	t.Helper()
	db, err := openStoreDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer db.Close()
	found := 0
	for i := 0; i < n; i++ {
		var one int
		err := db.QueryRowContext(context.Background(),
			`SELECT 1 FROM distilled_qa WHERE substr(question,1,?)=?`, dedupQuestionRunes,
			truncateRunes(batchPatchRow(i).Question, dedupQuestionRunes)).Scan(&one)
		if err == nil {
			found++
		}
	}
	return found
}

// installBatchFailTrigger arms the deterministic ROW-LEVEL mid-apply failure: an
// AFTER INSERT trigger on distilled_qa counts inserts in a side table and
// RAISE(ABORT)s on the Nth. The recipe is memory #5589's (RAISE(ABORT) trigger
// injection — how to fail an import at an EXACT row without manufacturing
// SQLITE_BUSY, which Rule 27 forbids).
//
// It runs on its OWN connection, opened and closed before the import, so the
// armed trigger cannot itself hold a write lock.
func installBatchFailTrigger(t *testing.T, dbPath string, failAfter int) {
	t.Helper()
	// Production's connection contract (Rule 13/14): the run's own handle must
	// queue behind a live writer rather than fail on the import's write lock.
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("trigger conn: %v", err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS batch_fail_counter (n INTEGER)`,
		`DELETE FROM batch_fail_counter`,
		`INSERT INTO batch_fail_counter (n) VALUES (0)`,
		fmt.Sprintf(`CREATE TRIGGER batch_count_trigger AFTER INSERT ON distilled_qa
			WHEN (SELECT n FROM batch_fail_counter) < %d
			BEGIN UPDATE batch_fail_counter SET n = n + 1; END`, failAfter),
		fmt.Sprintf(`CREATE TRIGGER batch_fail_trigger AFTER INSERT ON distilled_qa
			WHEN (SELECT n FROM batch_fail_counter) >= %d
			BEGIN SELECT RAISE(ABORT, 'batch-injected-failure'); END`, failAfter),
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("arm trigger (%s): %v", s, err)
		}
	}
}

// dropBatchFailTriggers removes the injection so the recovery re-run completes.
func dropBatchFailTriggers(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("drop trigger conn: %v", err)
	}
	defer db.Close()
	for _, s := range []string{
		`DROP TRIGGER IF EXISTS batch_fail_trigger`,
		`DROP TRIGGER IF EXISTS batch_count_trigger`,
		`DROP TABLE IF EXISTS batch_fail_counter`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("drop (%s): %v", s, err)
		}
	}
}

// TestMemoryAssetsImportBatchesBoundTheLock — THE RED-PROOF of the defect, and
// its measurement: the batch-boundary seam asks a SECOND connection for the write
// lock on both sides of every commit, so the test knows exactly what a concurrent
// writer would experience at each boundary (the key is copied before the seam
// returns — a t.Fatalf inside the seam would unwind the import's goroutine, not
// the test's).
//
// The defect's quantity is the BEFORE side: with one transaction per patch —
// RED-PROOF-START: wrap the whole loop in a single BeginTx and re-run this test —
// every row of the corpus sits inside ONE hold, so the before-commit probe is
// refused at every boundary and the after-commit probe never runs (there is no
// after-commit until the very end). RED-PROOF-END
//
// With per-batch transactions the lock is live at the before boundary and gone
// at the after boundary of the SAME batch — a property of EVERY batch, which the
// count below asserts for all of them at once.
func TestMemoryAssetsImportBatchesBoundTheLock(t *testing.T) {
	dbPath := batchTestDB(t, 1)
	patch := batchPatch(batchTestRows)
	wantBatches := (batchTestRows + memoryAssetImportBatchRows - 1) / memoryAssetImportBatchRows

	type obs struct {
		heldBefore, heldAfter int // boundary indexes where the lock was still live
		seen                  int // boundaries observed
	}
	var o obs
	oldSeam := memoryImportBatchBoundary
	memoryImportBatchBoundary = func(batch int, phase memoryImportBoundaryPhase) bool {
		held := peerLedgerLockHeld(t, dbPath)
		if phase == memoryImportBeforeCommit {
			o.seen++
			if held {
				o.heldBefore++
			}
			return true
		}
		if held {
			o.heldAfter++
		}
		return true
	}
	t.Cleanup(func() { memoryImportBatchBoundary = oldSeam })

	stats, err := MemoryAssetsImportPatch(patch, dbPath)
	if err != nil {
		t.Fatalf("batched import must succeed: %v", err)
	}
	if stats.Batches != wantBatches {
		t.Fatalf("batches=%d, want %d (batch size %d, %d rows)", stats.Batches, wantBatches, memoryAssetImportBatchRows, batchTestRows)
	}
	if stats.Imported != batchTestRows {
		t.Fatalf("imported=%d, want %d (a corpus the store collapses measures the dedup path, not the write path)", stats.Imported, batchTestRows)
	}
	if got := batchPatchCount(t, dbPath, batchTestRows); got != batchTestRows {
		t.Fatalf("target holds %d of %d patch rows", got, batchTestRows)
	}
	t.Logf("boundaries=%d lock_live_at_before_commit=%d lock_live_at_after_commit=%d batches=%d", o.seen, o.heldBefore, o.heldAfter, stats.Batches)
	if o.seen != wantBatches {
		t.Fatalf("observed %d before-commit boundaries, want %d (one per batch)", o.seen, wantBatches)
	}
	// INSIDE a held batch the ledger is locked — that is the honest, bounded cost
	// of any row-at-a-time import, and it must be visible (otherwise the seam is
	// not standing where it claims to).
	if o.heldBefore != wantBatches {
		t.Fatalf("the write lock was live at only %d of %d before-commit boundaries — the seam is not observing the batch's own transaction", o.heldBefore, wantBatches)
	}
	// ...and it is RELEASED at every commit: the property this fix adds. One
	// straggler is tolerated (a concurrent reader's shared lock can briefly
	// refuse an IMMEDIATE), but a per-patch transaction fails this outright: no
	// after-commit boundary exists until the whole corpus has been applied.
	if o.heldAfter > 1 {
		t.Fatalf("the write lock was still live at %d of %d after-commit boundaries — the import is holding the ledger across batches (the pre-2026-09-26 defect: one transaction for the whole corpus)", o.heldAfter, wantBatches)
	}
}

// TestMemoryAssetsImportPartialRerunCompletes — the failure semantics and the
// contract ruling, pinned deterministically. Two independent injections, both
// landing INSIDE an apply (no timing dependence, Rule 27):
//
//	① the boundary seam refuses batch 2 at its pre-commit side → batches before
//	  it are COMMITTED, so a partial state is a whole number of batches and IS
//	  durable (this is the half of the ruling an operator sees);
//	② a SQLite trigger RAISE(ABORT) at row 40 (inside batch 2) → the failing
//	  batch rolls back whole and the error names the failing patch row.
//
// Then the recovery: re-running the SAME patch completes it, nothing duplicated
// and nothing overwritten (unique-only + insert-only idempotence).
func TestMemoryAssetsImportPartialRerunCompletes(t *testing.T) {
	// ① seam-injected pre-commit failure at batch 2.
	dbPath := batchTestDB(t, 1)
	patch := batchPatch(batchTestRows)
	const failBatch = 2
	oldSeam := memoryImportBatchBoundary
	memoryImportBatchBoundary = func(batch int, phase memoryImportBoundaryPhase) bool {
		return !(batch == failBatch && phase == memoryImportBeforeCommit)
	}
	t.Cleanup(func() { memoryImportBatchBoundary = oldSeam })

	stats, err := MemoryAssetsImportPatch(patch, dbPath)
	if err == nil {
		t.Fatal("a batch refused at its pre-commit boundary must fail the import (Rule 15: never a silent partial success)")
	}
	wantCommitted := (failBatch - 1) * memoryAssetImportBatchRows
	if stats == nil {
		t.Fatal("a partial apply must still return its stats (what landed, and how far it got)")
	}
	if stats.RowsCommitted != wantCommitted {
		t.Fatalf("rows committed=%d, want %d (whole batches before the failing batch %d)", stats.RowsCommitted, wantCommitted, failBatch)
	}
	if stats.BatchesFailed != 1 {
		t.Fatalf("batches failed=%d, want 1 (a reported count nothing assigns is a defect)", stats.BatchesFailed)
	}
	if stats.Batches != failBatch-1 {
		t.Fatalf("batches committed=%d, want %d", stats.Batches, failBatch-1)
	}
	for _, want := range []string{
		strconv.Itoa(wantCommitted) + " of " + strconv.Itoa(batchTestRows),
		"NOT applied",
		"Re-run",
		"rolled back cleanly",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("failure message must carry %q (honest partial reporting): %v", want, err)
		}
	}
	if got := batchPatchCount(t, dbPath, batchTestRows); got != wantCommitted {
		t.Fatalf("target holds %d patch rows after the failure, want exactly the committed prefix %d", got, wantCommitted)
	}

	// ② row-level injection on its OWN fresh DB, so the trigger's insert counter
	// and the patch's row numbering coincide (a resumed patch would offset them).
	dbRowPath := batchTestDB(t, 1)
	installBatchFailTrigger(t, dbRowPath, batchTestFailRow)
	defer dropBatchFailTriggers(t, dbRowPath)
	statsRow, errRow := MemoryAssetsImportPatch(patch, dbRowPath)
	if errRow == nil {
		t.Fatal("the trigger injection must fail the import")
	}
	if !strings.Contains(errRow.Error(), "batch-injected-failure") {
		t.Errorf("the row-level failure must surface SQLite's own abort message: %v", errRow)
	}
	// Row 40 is the 41st patch row → batch 1 (rows 0..31) committed, batch 2
	// rolled back whole at its 9th row.
	if statsRow.RowsCommitted != memoryAssetImportBatchRows {
		t.Fatalf("row-level abort committed %d rows, want %d (batch 1 only: the failing batch rolls back whole)", statsRow.RowsCommitted, memoryAssetImportBatchRows)
	}
	if got := batchPatchCount(t, dbRowPath, batchTestRows); got != memoryAssetImportBatchRows {
		t.Fatalf("after the row-level abort the target holds %d patch rows, want the untouched prefix %d", got, memoryAssetImportBatchRows)
	}

	// Recovery: the SAME sync, re-run with both injections removed. Unique-only
	// + insert-only ⇒ the committed prefix is skipped as `existing`, the rest
	// lands.
	memoryImportBatchBoundary = oldSeam
	stats2, err := MemoryAssetsImportPatch(patch, dbPath)
	if err != nil {
		t.Fatalf("re-run after a partial apply must complete the patch: %v", err)
	}
	if stats2.Imported+stats2.Existing != batchTestRows {
		t.Fatalf("re-run touched %d of %d rows (imported=%d existing=%d deduped=%d refused=%d) — every row must be either newly imported or already present",
			stats2.Imported+stats2.Existing, batchTestRows, stats2.Imported, stats2.Existing, stats2.Deduped, stats2.Refused)
	}
	if stats2.Imported != batchTestRows-wantCommitted {
		t.Fatalf("re-run imported=%d, want %d — exactly the rows the partial apply had NOT committed (existing=%d)", stats2.Imported, batchTestRows-wantCommitted, stats2.Existing)
	}
	if stats2.Deduped != 0 {
		t.Fatalf("re-run deduped=%d, want 0 — the committed prefix must be recognised by the exact-question identity probe, not merged as a near-duplicate", stats2.Deduped)
	}
	if got := batchPatchCount(t, dbPath, batchTestRows); got != batchTestRows {
		t.Fatalf("after the recovery re-run the target holds %d of %d rows", got, batchTestRows)
	}
	// Idempotence: a further run changes nothing.
	stats3, err := MemoryAssetsImportPatch(patch, dbPath)
	if err != nil {
		t.Fatalf("converged re-run: %v", err)
	}
	if stats3.Imported != 0 || stats3.Deduped != 0 {
		t.Fatalf("converged re-run imported=%d deduped=%d, want 0/0", stats3.Imported, stats3.Deduped)
	}
	if stats3.Existing != batchTestRows {
		t.Fatalf("converged re-run existing=%d, want %d (every row present, none touched)", stats3.Existing, batchTestRows)
	}
}

// TestMemoryAssetsImportBatchLockScopeIsOneBatch — the INSTRUMENT's own
// calibration, and the reason the measurement above means something: at a
// batch's pre-commit boundary the write lock is held, and at the SAME batch's
// post-commit boundary it is already gone. Those two observations, one batch
// apart, are the lock SCOPE the fix produces — a batch, not a corpus.
//
// The control at the end answers the instrument's own validity question: after
// the import has returned, the same probe acquires the lock immediately, so a
// refusal earlier was the import holding it and not a broken probe.
func TestMemoryAssetsImportBatchLockScopeIsOneBatch(t *testing.T) {
	dbPath := batchTestDB(t, 1)
	patch := batchPatch(batchTestRows)
	const watchBatch = 2

	before := new(batchBracket)
	after := new(batchBracket)
	oldSeam := memoryImportBatchBoundary
	memoryImportBatchBoundary = func(batch int, phase memoryImportBoundaryPhase) bool {
		if batch != watchBatch {
			return true
		}
		held := peerLedgerLockHeld(t, dbPath)
		if phase == memoryImportBeforeCommit {
			before.record(held)
			return true
		}
		after.record(held)
		return true
	}
	t.Cleanup(func() { memoryImportBatchBoundary = oldSeam })

	if _, err := MemoryAssetsImportPatch(patch, dbPath); err != nil {
		t.Fatalf("import must succeed: %v", err)
	}
	b, a := before.get(), after.get()
	t.Logf("batch %d: locked_before_commit=%v locked_after_commit=%v", watchBatch, b, a)
	if !b {
		t.Fatalf("the write lock was NOT held at batch %d's pre-commit boundary — the seam is not inside the batch's transaction, so the lock-scope claim is unproven", watchBatch)
	}
	if a {
		t.Fatalf("the write lock was STILL held at batch %d's post-commit boundary — a committed batch must release the peer's ledger", watchBatch)
	}
	// Control: the probe can acquire the lock at all (otherwise the two
	// observations above are an instrument failure, not a measurement).
	if peerLedgerLockHeld(t, dbPath) {
		t.Fatal("the control probe was refused after the import finished — the probe cannot take the lock, so its earlier refusals prove nothing")
	}
}

// batchBracket carries one seam observation out of the import's goroutine
// without touching *testing.T from inside it.
type batchBracket struct {
	held bool
}

func (b *batchBracket) record(held bool) { b.held = held }

func (b *batchBracket) get() bool { return b.held }
