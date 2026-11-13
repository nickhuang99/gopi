package tools

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// LEDGER SCHEMA UNION — the regression suite for ledger_schema_union.go.
//
// The defect these tests pin (measured 2026-10-27, gopi 3b1a158): the chat
// index and the tasks ledger open the SAME file by default (chatDBPath() ==
// memoryDBPath()), and each opener used to create only its own family of
// objects. So the FIRST opener published the file's identity, and the other
// face refused it for ever:
//
//	gopi session list   (fresh custom GOPI_MEMORY_DB)  → exit 0, file left with
//	                                                      10 chats-family objects
//	gopi task list      (same path)                    → exit 1
//	  "is not a distilled knowledge base (missing distilled_qa table)"
//
// The fix is an opener semantic, not a retry: an opener CREATES the union of
// the faces, and an opener that meets a file carrying ONLY gopi's own objects
// ADOPTS it. A file carrying anything else stays refused (hermes'
// distilled_knowledge.db — distilled_qa without memory_feedback).

// ledgerUnionChatRowInsert is the fixture row that proves a chats row SURVIVES
// the heal: adoption must complete the ledger, never rebuild it.
const ledgerUnionChatRowInsert = `INSERT INTO chats(session_id, mode, title, started_at)
	VALUES('sess-union-1', ?, 'a conversation that must survive', 1.0)`

// TestChatOpenerThenTasksOpenerOnAFreshLedger is the epic's headline case: the
// chat index opens a brand-new shared ledger FIRST, and the tasks ledger must
// then accept the very same file. It fails on the pre-fix code (openChatDB
// created chats-only objects, so openTasksDB refused with "missing
// distilled_qa table").
func TestChatOpenerThenTasksOpenerOnAFreshLedger(t *testing.T) {
	path := testutil.NewTestDBPath(t) // Rule 32 scaffold: pins GOPI_MEMORY_DB
	ctx := context.Background()

	// ① the chat index is the FIRST opener of a file that does not exist yet.
	chat, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("openChatDB on a fresh shared ledger: %v", err)
	}
	chatRow, err := chat.Exec(ledgerUnionChatRowInsert, ChatModeCLI)
	if err != nil {
		chat.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("insert a chats row: %v", err)
	}
	affected, affErr := chatRow.RowsAffected()
	if affErr != nil {
		chat.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("chats row insert: rows affected: %v", affErr)
	}
	if affected != 1 {
		chat.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("chats row insert affected %d rows, want 1", affected)
	}
	chat.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs

	// ② the tasks ledger opens the SAME file. This is the assertion the bug
	// made impossible.
	tasks, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB after the chat index opened the same file: %v", err)
	}
	tasks.Close() //stylegate:ignore mid-test probe handle closed before the entry point runs

	// ③ and the real entry point — not just the opener — lands a row.
	if _, err := TaskNew("a row that must land", "", "", ""); err != nil {
		t.Fatalf("TaskNew on the chat-opened ledger: %v", err)
	}

	// ④ the heal COMPLETED the file; it did not rebuild it. The chats row the
	// first opener wrote is still there, which is what "adopt, never mutate
	// into emptiness" has to mean in practice.
	chat2, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("reopen the chat index after the heal: %v", err)
	}
	defer chat2.Close() //stylegate:ignore process-lifetime handle closed when the test ends
	var title string
	if err := chat2.QueryRowContext(ctx,
		`SELECT title FROM chats WHERE session_id = 'sess-union-1'`).Scan(&title); err != nil {
		t.Fatalf("the chats row did not survive the ledger heal: %v", err)
	}
	if title != "a conversation that must survive" {
		t.Errorf("chats row title = %q, want the row the first opener wrote", title)
	}
}

// TestChatsOnlyLedgerIsAdoptedByTheTasksOpener pins the HISTORICAL shape: a
// file already left behind by the pre-fix chat opener (chats-family objects
// only, no distilled_qa). It is built WITHOUT the union door — openDB +
// ensureChatSchema is exactly what openChatDB used to do — so this test
// describes the defect's residue, not the fix's output.
func TestChatsOnlyLedgerIsAdoptedByTheTasksOpener(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	ctx := context.Background()

	legacy, err := openDB(path, false) // the pre-fix opener's two steps
	if err != nil {
		t.Fatalf("open the legacy file: %v", err)
	}
	if err := ensureChatSchema(ctx, legacy); err != nil {
		legacy.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("build the chats-only schema: %v", err)
	}
	legacy.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs

	// Pre-condition of the regression: the file really is chats-only. If this
	// ever stops holding, the test below would pass for the wrong reason.
	probe, err := openDB(path, true)
	if err != nil {
		t.Fatalf("probe the legacy file: %v", err)
	}
	hasKB, err := tableExists(ctx, probe, knowledgeBaseAnchorTable)
	probe.Close() //stylegate:ignore read-only probe handle closed after its single query
	if err != nil {
		t.Fatalf("probe distilled_qa: %v", err)
	}
	if hasKB {
		t.Fatal("the fixture is not chats-only — it already carries distilled_qa")
	}

	tasks, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB must ADOPT a file carrying only gopi's own objects: %v", err)
	}
	tasks.Close() //stylegate:ignore mid-test probe handle closed before the entry point runs

	if _, err := TaskNew("a row in an adopted ledger", "", "", ""); err != nil {
		t.Fatalf("TaskNew on the adopted ledger: %v", err)
	}
}

// TestChatsOnlyLedgerIsHealedByTheChatOpener is the same residue reached by the
// chat face: opening the chat index on a pre-fix file leaves it a LEDGER, so
// the next face finds a knowledge base instead of a foreign file.
func TestChatsOnlyLedgerIsHealedByTheChatOpener(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	ctx := context.Background()

	legacy, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open the legacy file: %v", err)
	}
	if err := ensureChatSchema(ctx, legacy); err != nil {
		legacy.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("build the chats-only schema: %v", err)
	}
	legacy.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs

	chat, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("openChatDB on a chats-only ledger: %v", err)
	}
	defer chat.Close() //stylegate:ignore process-lifetime handle closed when the test ends
	hasKB, err := tableExists(ctx, chat, knowledgeBaseAnchorTable)
	if err != nil {
		t.Fatalf("probe distilled_qa after the chat open: %v", err)
	}
	if !hasKB {
		t.Error("the chat opener left the shared file without distilled_qa — the union rule did not run")
	}
}

// TestLedgerUnionLeavesForeignFilesAlone is the boundary that must NOT move:
// a file carrying ANY object gopi did not mint is somebody else's database.
// The tasks opener still refuses it, and the chat opener still lands only its
// own tables there (the pre-existing behaviour for a foreign file — this fix
// changes which files are OURS, not what we do with someone else's).
func TestLedgerUnionLeavesForeignFilesAlone(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	ctx := context.Background()

	legacy, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open the foreign file: %v", err)
	}
	if err := ensureChatSchema(ctx, legacy); err != nil {
		legacy.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("build the chats schema: %v", err)
	}
	if _, err := legacy.Exec(`CREATE TABLE state (id INTEGER PRIMARY KEY, blob TEXT)`); err != nil {
		legacy.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("plant the foreign table: %v", err)
	}
	legacy.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs

	if _, err := openTasksDB(ctx, path); err == nil {
		t.Fatal("openTasksDB adopted a file holding a foreign table — 'never mutate foreign DBs' was widened")
	} else if !strings.Contains(err.Error(), "missing distilled_qa table") {
		t.Errorf("the refusal must stay the named one, got: %v", err)
	}

	// And the chat opener must not build a knowledge base inside it either.
	chat, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("openChatDB on a foreign file (it lands only chats): %v", err)
	}
	defer chat.Close() //stylegate:ignore process-lifetime handle closed when the test ends
	hasKB, err := tableExists(ctx, chat, knowledgeBaseAnchorTable)
	if err != nil {
		t.Fatalf("probe distilled_qa in the foreign file: %v", err)
	}
	if hasKB {
		t.Error("the chat opener created distilled_qa inside a foreign database")
	}
}

// TestHermesShapedLedgerIsStillRefused pins the constraint's own example: the
// hermes production copy has distilled_qa but no memory_feedback, and the
// adoption rule must not make it acceptable. (tasks_test.go's
// TestTasksRejectForeignDB covers the same refusal for a store-built file; this
// one states it in the union suite so the constraint cannot be satisfied by
// deleting that older test.)
func TestHermesShapedLedgerIsStillRefused(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	ctx := context.Background()

	db, err := openStoreDB(ctx, path) // a real knowledge base …
	if err != nil {
		t.Fatalf("openStoreDB: %v", err)
	}
	// … with the hermes shape: distilled_qa present, memory_feedback gone.
	if _, err := db.Exec(`CREATE TABLE memory_feedback_placeholder (id INTEGER)`); err != nil {
		db.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("placeholder: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE memory_feedback`); err != nil {
		db.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs
		t.Fatalf("drop memory_feedback: %v", err)
	}
	db.Close() //stylegate:ignore mid-test probe handle closed before the next opener runs

	if _, err := openTasksDB(ctx, path); err == nil {
		t.Fatal("openTasksDB accepted a foreign distilled DB (distilled_qa, no memory_feedback)")
	} else if !strings.Contains(err.Error(), "foreign distilled DB") {
		t.Errorf("the hermes shape must be refused as a foreign distilled DB, got: %v", err)
	}
}

// TestLedgerUnionRecognizesEveryGopiObject is the anti-drift guard (Rule 13's
// shape): it BUILDS a fully-populated gopi ledger — every face's schema, no
// rows needed — and asserts the MEASURED reference set recognizes every object
// in it, both directions. A new index face, a renamed table or a new FTS
// companion turns this red, which is the only thing standing between "adopt our
// own partial file" and "adopt a file we do not understand".
func TestLedgerUnionRecognizesEveryGopiObject(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	ctx := context.Background()

	db, err := ensureKnowledgeBase(ctx, path, false)
	if err != nil {
		t.Fatalf("ensureKnowledgeBase: %v", err)
	}
	for _, ensure := range []func(context.Context, *sql.DB) error{
		ensureTasksSchema, ensureChatSchema, ensureChatNotesSchema,
		ensureProceduralSchema, ensureSessionConclusionsSchema,
	} {
		if err := ensure(ctx, db); err != nil {
			db.Close() //stylegate:ignore mid-test probe handle closed before the census runs
			t.Fatalf("build a face's schema: %v", err)
		}
	}
	actual, err := readLedgerObjectNames(ctx, db)
	if err != nil {
		db.Close() //stylegate:ignore mid-test probe handle closed after its last query
		t.Fatalf("census: %v", err)
	}
	db.Close() //stylegate:ignore mid-test probe handle closed after its last query

	// A scan that found nothing must not pass (Rule 15/38: an empty census is
	// indistinguishable from a clean one).
	if len(actual) < ledgerUnionCensusFloor {
		t.Fatalf("the census read %d objects, below the floor %d — it did not measure a real ledger", len(actual), ledgerUnionCensusFloor)
	}

	reference, err := gopiLedgerObjectNames(ctx)
	if err != nil {
		t.Fatalf("reference set: %v", err)
	}

	// Direction ①: every object this real ledger carries is recognized. A miss
	// here is the dangerous one — the adoption census would call gopi's OWN
	// ledger foreign (a regression, not a refusal).
	var unrecognized []string
	for name := range actual {
		if _, ok := reference[name]; !ok {
			unrecognized = append(unrecognized, name)
		}
	}
	sort.Strings(unrecognized)
	if len(unrecognized) > 0 {
		t.Fatalf("%d ledger object(s) are missing from the reference set — the adoption census would call a gopi ledger foreign: %v",
			len(unrecognized), unrecognized)
	}

	// Direction ②: the reference set claims NOTHING the real ledger does not
	// carry. A reference padded with a name no face mints would be an
	// accidental permit for a foreign database that happens to use that name.
	var phantom []string
	for name := range reference {
		if _, ok := actual[name]; !ok {
			phantom = append(phantom, name)
		}
	}
	sort.Strings(phantom)
	if len(phantom) > 0 {
		t.Fatalf("%d reference name(s) exist in no real ledger — the reference build measured something the faces do not mint: %v",
			len(phantom), phantom)
	}

	// And the answer is a SET, not a rubber stamp: a foreign name is not in it.
	for _, foreign := range []string{"state", "distilled", "tasks_archive_legacy_unknown", ""} {
		if _, ok := reference[foreign]; ok {
			t.Errorf("%q is in the reference set — a foreign name must not be adopted", foreign)
		}
	}
}

// ledgerUnionCensusFloor is the smallest object count a fully-built gopi
// ledger can plausibly have (measured well above this: the knowledge base
// alone builds 20+ objects). It exists so a census that read NOTHING is a
// failure rather than a vacuous pass.
const ledgerUnionCensusFloor = 20

// TestLedgerObjectCensusDistinguishesShapes drives the predicate the adoption
// decision rests on, on both sides of the boundary and on the error path.
func TestLedgerObjectCensusDistinguishesShapes(t *testing.T) {
	ctx := context.Background()

	t.Run("empty file is not ours", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.db")
		db, err := openDB(path, false)
		if err != nil {
			t.Fatalf("openDB: %v", err)
		}
		defer db.Close() //stylegate:ignore probe handle closed when the subtest ends
		ours, err := ledgerHoldsOnlyGopiObjects(ctx, db)
		if err != nil {
			t.Fatalf("census: %v", err)
		}
		if ours {
			t.Error("an empty file answered TRUE — that shape belongs to adoptUninitializedKnowledgeBase")
		}
	})

	t.Run("gopi objects are ours", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gopi.db")
		db, err := openDB(path, false)
		if err != nil {
			t.Fatalf("openDB: %v", err)
		}
		defer db.Close() //stylegate:ignore probe handle closed when the subtest ends
		if err := ensureChatSchema(ctx, db); err != nil {
			t.Fatalf("ensureChatSchema: %v", err)
		}
		ours, err := ledgerHoldsOnlyGopiObjects(ctx, db)
		if err != nil {
			t.Fatalf("census: %v", err)
		}
		if !ours {
			t.Error("a chats-only file (gopi's own pre-fix residue) answered FALSE — it would never be adopted")
		}
	})

	t.Run("one foreign object makes the file foreign", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mixed.db")
		db, err := openDB(path, false)
		if err != nil {
			t.Fatalf("openDB: %v", err)
		}
		defer db.Close() //stylegate:ignore probe handle closed when the subtest ends
		if err := ensureChatSchema(ctx, db); err != nil {
			t.Fatalf("ensureChatSchema: %v", err)
		}
		if _, err := db.Exec(`CREATE TABLE somebody_elses (id INTEGER PRIMARY KEY)`); err != nil {
			t.Fatalf("plant: %v", err)
		}
		ours, err := ledgerHoldsOnlyGopiObjects(ctx, db)
		if err != nil {
			t.Fatalf("census: %v", err)
		}
		if ours {
			t.Error("a file with one foreign table answered TRUE — adoption would mutate a foreign DB")
		}
	})
}
