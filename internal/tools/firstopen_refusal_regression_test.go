package tools

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const refusalObservationWindows = 3

// testSQLiteMasterCountSQL counts a SQLite file's schema objects. It is the
// TEST's own probe, deliberately not the production constant: the red-before
// proof must compile against the PRE-fix tree, where that helper did not exist
// yet (Rule 1b — one definition, here the fixture's).
const testSQLiteMasterCountSQL = `SELECT count(*) FROM sqlite_master`

// testFileHasNoSchemaObjects reports whether path is an empty SQLite file
// (zero schema objects — nobody's schema published yet).
func testFileHasNoSchemaObjects(t *testing.T, path string) bool {
	t.Helper()
	db, err := openDB(path, false)
	if err != nil {
		t.Fatalf("window probe open: %v", err)
	}
	defer db.Close() //stylegate:ignore probe handle closed immediately after its single read
	var n int
	if err := db.QueryRow(testSQLiteMasterCountSQL).Scan(&n); err != nil {
		t.Fatalf("window probe read: %v", err)
	}
	return n == 0
}

// The two tests below are the regression guard for the READ-side half of the// concurrent-first-open defect (t-mem-firstopen-refusal): a store/search
// arriving at a knowledge base whose FIRST OPEN has not been published yet was
// REFUSED — "memory_store: <path> is not a distilled knowledge base (missing
// distilled_qa table)" — because the loser's recovery was a wall-clock poll
// (waitForDistilledSchema, dbSchemaWaitTries×dbSchemaWaitInterval = 500ms)
// while the winner's build is a SEQUENCE of write transactions whose commits
// are device syncs (measured max 837ms; internal/tools/firstopen_probe_test.go).
// The poll's budget is therefore not a bound on the build, and every real
// waiter was refused whenever the build outlasted it.
//
// Both tests are RED on the pre-fix code and GREEN after the recovery became a
// fact about the FILE (hasNoSchemaObjects: zero schema objects = nobody's data,
// so the opener joins the initialization) instead of a guess about another
// process's progress.

// TestEmptyFileStoreIsInitializedNotRefused is the minimal deterministic
// reproducer: an EMPTY file (zero bytes — the exact shape a first open is
// observable in, because the schema batch commits atomically) at a CUSTOM
// path must be adopted and initialized, not refused as a foreign DB.
//
// This is the smallest sample that separates the two shapes an
// "uninitialized" file can have, and it is where the contract had to change:
// before the fix the custom-path branch refused ANY file lacking distilled_qa,
// so an empty one was indistinguishable from hermes' populated database.
// The negative half of the same probe (a file WITH a foreign table must stay
// refused, byte-for-byte unmutated) is asserted here too, so the fix cannot
// drift into "initialize anything that lacks the table" — that would mutate a
// foreign DB, the contract the refusal exists to keep.
func TestEmptyFileStoreIsInitializedNotRefused(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memoryDBEnv, empty)

	if _, err := MemoryStore("empty-file topic", "is an empty file adoptable?",
		"yes — zero schema objects means nobody's data is there", "src-empty", nil); err != nil {
		t.Fatalf("store into an EMPTY custom-path file must initialize it, not refuse it: %v", err)
	}
	out, err := MemorySearch("empty-file", 5, "")
	if err != nil {
		t.Fatalf("search after the adopted store: %v", err)
	}
	if !strings.Contains(out, "empty-file topic") {
		t.Fatalf("the row stored into the adopted file is not searchable: %s", out)
	}

	// Negative probe: the SAME code path, one table different — a foreign DB
	// with content must still be refused, and must not be mutated.
	foreign := filepath.Join(dir, "state.db")
	fdb := openRawDB(t, foreign)
	seed(t, fdb, "CREATE TABLE junk (x TEXT)")
	fdb.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	before, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(memoryDBEnv, foreign)
	_, err = MemoryStore("t", "q", "a", "", nil)
	if err == nil || !strings.Contains(err.Error(), "not a distilled knowledge base") {
		t.Fatalf("a populated foreign DB must still be refused, got: %v", err)
	}
	after, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("the foreign DB was mutated by the refused store")
	}
}

// TestConcurrentFirstOpenWaiterIsNotRefused drives the EXACT window: an
// initializer is parked inside the atomic schema transaction (via the
// schemaBatchProbeHook seam, i.e. after the base table DDL and before COMMIT,
// holding the file empty from every other connection's point of view) while a
// second opener arrives. The publisher is released only AFTER the second
// opener has returned, so its verdict was provably reached on an EMPTY file —
// the window the defect lives in, established by the test's own ordering, not
// by a timing guess. The second opener must come back with a usable knowledge
// base instead of the refusal.
func TestConcurrentFirstOpenWaiterIsNotRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)

	// pollBudget is what a refusal costs the pre-fix code; the observation
	// window is a small multiple of it, so a refusal lands INSIDE the window
	// (red) and a join is still waiting when it closes (green).
	pollBudget := time.Duration(dbSchemaWaitTries) * dbSchemaWaitInterval

	inBatch := make(chan struct{})
	release := make(chan struct{})
	var parked sync.Once
	// Park the FIRST batch (the publisher's) inside its transaction; every
	// later batch (the recovery's own build) runs straight through.
	schemaBatchProbeHook = func() {
		parked.Do(func() {
			close(inBatch)
			<-release
		})
	}
	t.Cleanup(func() { schemaBatchProbeHook = nil })

	publisherDone := make(chan error, 1)
	go func() {
		db, err := ensureKnowledgeBase(context.Background(), path, false)
		if db != nil {
			db.Close() //stylegate:ignore fixture handle; the initialized FILE is what the test exercises
		}
		publisherDone <- err
	}()

	select {
	case <-inBatch:
	case <-time.After(30 * time.Second):
		t.Fatal("publisher never reached its schema batch")
	}

	// The window, observed: the file exists and has published NOTHING.
	probe, err := openDB(path, false)
	if err != nil {
		t.Fatalf("probe open: %v", err)
	}
	published, perr := tableExists(context.Background(), probe, "distilled_qa")
	probe.Close() //stylegate:ignore probe handle closed immediately after its single read
	if perr != nil {
		t.Fatalf("probe read: %v", perr)
	}
	if published {
		t.Fatal("the parked publisher's schema is already visible — the window this test targets is not open")
	}

	// The second opener, launched while the publisher is parked. The
	// publisher holds the per-path init lock inside its parked transaction,
	// so the file is provably EMPTY for the whole of this observation: any
	// verdict it reaches here is the defect's verdict.
	loserDone := make(chan error, 1)
	var loserDB *sql.DB
	go func() {
		db, err := openStoreDB(context.Background(), path)
		loserDB = db
		loserDone <- err
	}()

	// The observation window is a DEADLINE, not a wait: the pre-fix code
	// refuses after spending its poll budget (dbSchemaWaitTries ×
	// dbSchemaWaitInterval), so an opener that has not CONCLUDED within a
	// small multiple of that budget did not refuse — it is waiting on the
	// peer, which is exactly the behaviour this test requires. A refusal
	// arriving inside the window (red on the pre-fix tip) fails here.
	select {
	case err := <-loserDone:
		if err != nil {
			t.Fatalf("the second opener was refused while the first was still building the very same file: %v", err)
		}
		t.Fatal("the second opener returned while the publisher was still parked — it cannot have initialized the file, so its verdict came from somewhere other than the join")
	case <-time.After(refusalObservationWindows * pollBudget):
		// Still working: it joined the initialization and is queued on the
		// publisher's lock. Expected.
	}

	// The publisher was NEVER released before that observation, so the file
	// the opener is deciding on is provably still empty — asserted here,
	// while the publisher is still parked (the window cannot close before
	// `release`).
	if !testFileHasNoSchemaObjects(t, path) {
		t.Fatal("the publisher published while the opener was deciding — this is not the window the defect lives in")
	}

	close(release)
	select {
	case err := <-loserDone:
		if err != nil {
			t.Fatalf("the second opener failed after the publisher released: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the second opener never returned after the publisher released")
	}
	defer loserDB.Close()
	select {
	case err := <-publisherDone:
		if err != nil {
			t.Fatalf("publisher: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the publisher never returned")
	}

	// The recovered handle is a REAL knowledge base: it stores and the row is
	// searchable through the shared path.
	if err := ensureSchema(context.Background(), loserDB); err != nil {
		t.Fatalf("recovered DB is not buildable: %v", err)
	}
	if _, err := MemoryStore("waiter topic", "does the waiter recover?",
		"yes — it joins the initialization instead of refusing", "src-waiter", nil); err != nil {
		t.Fatalf("store through the recovered opener: %v", err)
	}
	out, err := MemorySearch("waiter", 5, "")
	if err != nil {
		t.Fatalf("search after the recovered store: %v", err)
	}
	if !strings.Contains(out, "waiter topic") {
		t.Fatalf("the row stored after recovery is not searchable: %s", out)
	}
}
