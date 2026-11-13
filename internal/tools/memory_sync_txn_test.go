package tools

// memory_sync_txn_test.go — Epic acceptance for the single-transaction
// import (devdoc/memsync-import-txn.md): TWO PROCESSES importing the SAME
// patch against the SAME DB concurrently must converge with no duplicates
// and no partial import — "恰一成功或全等幂等":
//
//   - both processes exit 0, and the DB holds exactly len(rows) rows
//     (the loser dedups into the winner's rows — busy_timeout queued it on
//     BEGIN IMMEDIATE, so it saw the winner's committed rows);
//   - or a loser fails EXPLICITLY (non-zero exit) with the DB holding only
//     the winner's complete row set — never a half-applied patch.
//
// The child mode (GOPI_TEST_IMPORT_HELPER=1) runs the real SyncImport —
// the same code path `gopi memory memsync --import` drives.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// syncImportTxnPatchRows is the patch size for the concurrency test — wide
// enough that two interleaved autocommit imports WOULD collide (pre-fix
// behavior: each row its own transaction), small enough to stay fast.
const syncImportTxnPatchRows = 20

func TestSyncImportTwoProcessConcurrent(t *testing.T) {
	if os.Getenv(syncImportHelperEnv) == "1" {
		// The helper runs under `-test.run ^TestSyncImportTwoProcessConcurrent$`
		// — which matches THIS function — so the `return` below is load-bearing:
		// without it the child would fall through into the parent body, spawn two
		// children of its own, and every level would import the same patch.
		// (Measured 2026-09-25: it does NOT fall through — a child logs
		// `rows_before` and `stored=1 deduped=19`, i.e. it ran THIS body.)
		syncImportHelperMain(t)
		return
	}

	dbPath := syncTestDB(t)

	// Materialize the schema BEFORE spawning children: the first-open race
	// (fileExists→initKnowledgeBase non-atomic) is a separate known issue
	// — this test targets the import transaction boundary, not first boot.
	if _, err := MemoryStore("seed", "seed the schema before the concurrent imports", "seed answer body with enough meaningful characters to pass the gate", "", nil); err != nil {
		t.Fatal(err)
	}

	patch := &SyncPatch{Version: syncPatchVersion, Node: "hp-concurrent"}
	for i := 0; i < syncImportTxnPatchRows; i++ {
		// ONE DISTINCT LESSON PER ROW — a distinct topic AND a distinct
		// retrieval surface. This test's subject is the whole-patch TRANSACTION
		// boundary under two concurrent importers (atomicity + idempotent
		// convergence), and the store's similarity screen is orthogonal to it:
		// 8ba90a29 (fix(memory): merge re-worded twin lessons) lowered the
		// twin-merge gate to Jaccard > lessonJaccardMerge with a shared-trigram
		// floor, and 20 rows built from one template differing only in an index
		// ARE re-worded twins by that rule — the import then legitimately
		// converges on fewer rows (measured 2026-09-25: stored=1 deduped=19,
		// row count 2 of 21), which is the dedup gate WORKING, not an
		// atomicity failure. A fixture that shared one topic measured that gate
		// instead of the boundary it names.
		patch.Rows = append(patch.Rows, SyncRow{
			Topic:    syncImportTxnTopic,
			Question: syncImportTxnQuestion(i),
			Answer:   syncImportTxnAnswer(i),
		})
	}
	patchPath := filepath.Join(t.TempDir(), "concurrent-patch.json")
	if err := WriteSyncPatch(patch, patchPath); err != nil {
		t.Fatal(err)
	}

	cmd := func() *exec.Cmd {
		c := exec.Command(os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v")
		// The ledger the child imports into is THIS test's (syncTestDB), and it
		// travels in the environment. Appending to os.Environ() is what makes
		// the child's TestMain print the suite pin's "… is INHERITED …" line:
		// the child resolves GOPI_MEMORY_DB from the ambient test environment
		// (testutil.PinSuiteForTestMain captures it before any t.Setenv) and
		// finds a ledger NAMED here, so the pin declines to overrule it.
		//
		// That line is the pin WORKING, not this fixture failing: overriding a
		// named ledger is the bug the pin's own regression test forbids
		// (testutil.TestLedgerPinHonoursANamedLedgerButRefusesTheOperatorsOwn —
		// "an unconditional pin sent the children elsewhere and the parent
		// counted rows in a DB nobody wrote"). It names no error and asserts no
		// failure, so nothing here treats it as one (Rule 15b: a report is not a
		// swallow). Measured 2026-09-25: rebuilding the env so the child carries
		// exactly one authority left every assertion below — and that line —
		// byte-for-byte unchanged, which is what settled the adjudication.
		c.Env = append(os.Environ(),
			syncImportHelperEnv+"=1",
			syncImportPatchEnv+"="+patchPath,
		)
		return c
	}

	type outcome struct {
		name   string
		err    error
		stdout string
		stderr string
	}
	results := make(chan outcome, 2)
	for n, spawn := range []func() *exec.Cmd{cmd, cmd} {
		go func(n int, spawn func() *exec.Cmd) {
			c := spawn()
			out, err := c.CombinedOutput()
			results <- outcome{name: fmt.Sprintf("process-%d", n+1), err: err, stdout: string(out)}
		}(n, spawn)
	}
	var outs []outcome
	for i := 0; i < 2; i++ {
		outs = append(outs, <-results)
	}

	accepted := 0
	for _, o := range outs {
		t.Logf("%s: err=%v output: %s", o.name, o.err, strings.TrimSpace(o.stdout))
		// The child's ledger authority, named by the pin itself. Appending to
		// os.Environ() means the child's TestMain resolves GOPI_MEMORY_DB from
		// the ambient test environment — testutil.PinSuiteForTestMain captures
		// it BEFORE any t.Setenv can be seen — and finds a ledger NAMED at exec
		// point, so it declines to overrule it and REPORTS which one it honoured
		// ("… is INHERITED"). That line is the pin working, not this fixture
		// failing: overruling a named ledger is the bug the pin's own regression
		// test forbids (testutil.TestLedgerPinHonoursANamedLedgerButRefusesTheOperatorsOwn
		// — "an unconditional pin sent the children elsewhere and the parent
		// counted rows in a DB nobody wrote"). Assert on it instead of tolerating
		// it, so the fixture's contract is explicit: each child must have been
		// told THIS test's ledger and no other. If a child is ever handed the
		// suite sandbox (the historic unconditional-pin bug) or a second
		// authority for the path, this path stops appearing and the assertion
		// fires here rather than as a mysterious row-count mismatch.
		if !strings.Contains(o.stdout, dbPath) {
			t.Errorf("%s never reported %s as its ledger — the child was handed a different ledger (the suite "+
				"pin honours one NAMED by the test, so its report must name this DB):\n%s",
				o.name, dbPath, strings.TrimSpace(o.stdout))
		}
		if o.err == nil {
			accepted++
		}
	}
	if accepted == 0 {
		t.Fatalf("both import processes failed — first failure: %v", outs[0].err)
	}

	// Convergence: exactly len(rows) distilled rows (the seed row included
	// → 1 + N), no duplicates, no half-patch.
	if got := countDistilledRows(t, dbPath); got != syncImportTxnPatchRows+1 {
		t.Errorf("row count after two concurrent imports: got %d, want %d (1 seed + %d patch rows — duplicates or a partial import broke atomicity)", got, syncImportTxnPatchRows+1, syncImportTxnPatchRows)
	}

	// Per-question uniqueness: each question exactly once.
	db, err := openSearchDB(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < syncImportTxnPatchRows; i++ {
		q := syncImportTxnQuestion(i)
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM distilled_qa WHERE question LIKE ?`, q+"%").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("question %q stored %d times (want 1) — the dedup key did not converge across processes", q, n)
		}
	}
}

const (
	syncImportHelperEnv = "GOPI_TEST_IMPORT_HELPER"
	syncImportPatchEnv  = "GOPI_TEST_IMPORT_PATCH"
)

// syncImportTxnTopic is the topic every patch row shares — the dedup screen is
// per-topic, so one topic is what makes the convergence claim meaningful (a
// per-row topic would make every row trivially un-mergeable and the test would
// stop exercising the screen at all).
const syncImportTxnTopic = "brew: concurrent txn acceptance"

// syncImportTxnSubjects are the DISTINCT subjects the patch's rows are about,
// one per row. They are not decoration: they are what keeps the fixture legal
// for a convergence test.
//
// WHY (measured on master, 2026-09-25): the store keeps only rows that are not
// near-copies of one already stored — memory.go's same-lesson screen merges a
// row into an existing same-topic row when the trigram Jaccard of
// `topic question answer` is > lessonJaccardMerge (0.40) AND the pair shares
// ≥ lessonMinSharedTrigrams (120) trigrams. A patch whose rows differed only
// by an index (same topic, "…question 01"/"…question 02", one shared
// boilerplate answer) is EXACTLY that shape: the first import reported
// `stored=1 deduped=19`, folding 19 of its OWN rows into row 1. The test's
// `want 21` was therefore unreachable no matter how correct the import
// transaction is — it was measuring the dedup screen, not the transaction.
//
// The subjects are deliberately long and lexically disjoint while the shared
// boilerplate stays short, so BOTH conditions of the screen fail by a wide
// margin (measured: Jaccard well under 0.40, shared trigrams well under 120).
// The array's length is the row-count constant, so a future edit cannot add a
// row without a subject (Rule 1/13 — the count lives once).
var syncImportTxnSubjects = [syncImportTxnPatchRows]string{
	"sqlite BEGIN IMMEDIATE serializes both importers on one database file",
	"the busy timeout queues the loser until the winner commits its patch",
	"dedup keys compare the first two hundred runes of a question text",
	"trigram jaccard similarity folds near copies of one lesson together",
	"the patch version gate refuses an import written by a newer binary",
	"a garbage row is refused by name while its neighbours still commit",
	"the ownership probe refuses a row a local human row already owns",
	"commit or rollback is all or nothing for every row in one patch",
	"the export excludes rows the receiving peer originally sent to us",
	"node tags ride the sources column and survive when sources merge",
	"the anchor window hides rows older than the last successful pull",
	"a rolled back patch leaves the database exactly as it was before",
	"the same patch imported twice is idempotent rather than duplicated",
	"row level semantic refusals are counted while the patch still commits",
	"the store opens one connection per process and never shares it",
	"keyword tail markers are stripped before similarity is compared",
	"the question column is trimmed before the dedup key is computed",
	"a partial import is impossible because the transaction wraps it all",
	"concurrent importers converge on the winner's committed row set",
	"the loser reports deduplicated rows rather than a failing exit code",
}

// syncImportTxnQuestion / syncImportTxnAnswer render row i's text. ONE
// definition (Rule 14): the patch builder and the per-question uniqueness
// assertion must speak about the SAME text, or the test's two halves drift and
// the assertion silently starts asking about rows nobody imported.
func syncImportTxnQuestion(i int) string {
	return fmt.Sprintf("txn convergence %02d: %s", i, syncImportTxnSubjects[i])
}

func syncImportTxnAnswer(i int) string {
	return fmt.Sprintf("%s — patch %02d commits as one transaction.", syncImportTxnSubjects[i], i)
}

// syncImportHelperMain is the child-process body: run the real import and
// exit non-zero on failure so the parent can assert the explicit-failure
// contract (a rolled-back import must not look like success).
func syncImportHelperMain(t *testing.T) {
	stats, err := SyncImport(os.Getenv(syncImportPatchEnv))
	if err != nil {
		fmt.Fprintf(os.Stderr, "import helper: %v\n", err)
		os.Exit(3)
	}
	fmt.Printf("stored=%d deduped=%d refused=%d\n", stats.RowsImported, stats.RowsDeduped, stats.RowsRefused)
}
