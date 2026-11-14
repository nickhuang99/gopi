package tools

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// ---------------------------------------------------------------------------
// The shared ledger's SHAPE is the UNION of its index faces.
// ---------------------------------------------------------------------------
//
// WHY THIS FILE EXISTS (the defect, measured 2026-10-27). Every feature face
// opens ONE database file — the knowledge base (distilled_qa …), the tasks
// ledger, the chat index, the round-note index — and each opener used to
// create only ITS OWN family of schema objects. Whichever face opened a file
// FIRST therefore decided what the file WAS, and the other faces had only two
// options left: refuse it as foreign, or mutate it behind the owner's back.
//
// Measured on a fresh custom ledger path (GOPI_MEMORY_DB=…/custom-ledger/
// memory.db, gopi's own binary at 3b1a158):
//
//	gopi session list   → exit 0, and the file is left carrying 10 objects,
//	                      ALL chats family (chats, chats_fts*, chats_tri*,
//	                      idx_chats_mode, idx_chats_started), zero distilled_qa
//	gopi task list      → exit 1
//	  "gopi task: …/custom-ledger/memory.db is not a distilled knowledge base
//	   (missing distilled_qa table)"
//
// — for ever, because the chat opener had already published the file as its
// own (openTasksDB's foreign-DB refusal is correct about what it can see).
//
// The fix is neither a bigger poll nor a retry: it is that the file has ONE
// shape. An opener that intends to CREATE the ledger builds the UNION; an
// opener that finds a file carrying ONLY gopi's own objects ADOPTS it (the
// same fact-not-guess recovery adoptUninitializedKnowledgeBase already uses
// for an empty file: read the FILE, never guess from elapsed time). A file
// carrying anything gopi did not create is still somebody else's database and
// is still refused — hermes' distilled_knowledge.db is not made more
// acceptable by any of this, because "never mutate foreign DBs" (tasks.go's
// refusal, extended from rows to schema) is what makes it read-only
// compatible.
//
// The "is it ours" question is answered by an EXACT object-name census, never
// by a name PREFIX: a prefix list would answer "ours" for any application
// whose own database happens to carry a `tasks` table, and adopting that file
// would build a knowledge base inside somebody else's DB — precisely the
// mutation the refusal exists to prevent. The honest answer is the one gopi
// can PROVE: the name set its own openers mint, measured from a reference
// build (gopiLedgerObjectNames) rather than typed into a list here.

const (
	// knowledgeBaseAnchorTable is the ONE table whose presence means "this
	// file is a distilled knowledge base" — the same anchor openTasksDB,
	// openSearchDB and openStoreDB test, named once (Rule 1b: a comparison
	// target is never typed inline twice).
	knowledgeBaseAnchorTable = "distilled_qa"

	// ledgerObjectNamesSQL lists every schema object a file carries. SQLite's
	// own internal objects (sqlite_sequence, sqlite_autoindex_*) are excluded:
	// they are the engine's bookkeeping, not a claim about who owns the file.
	ledgerObjectNamesSQL = `SELECT name FROM sqlite_master WHERE name NOT LIKE '` + sqliteInternalObjectPrefix + `%'`

	// sqliteInternalObjectPrefix is the engine's own object namespace.
	sqliteInternalObjectPrefix = "sqlite_"

	// ledgerReferenceDBDSN is the throwaway database the reference object set
	// is built in. It is IN MEMORY on purpose: the reference is a shape, not a
	// file, and a shape that touches no disk cannot leave residue behind
	// (Rule 22 has nothing to say about a file that never exists).
	ledgerReferenceDBDSN = ":memory:"
)

// gopiLedgerObjectNames returns the set of schema object names gopi's own
// openers mint into the shared ledger — the reference answer to "is this file
// ours?".
//
// It is MEASURED, once per process, by building every face's schema into a
// throwaway in-memory database through the SAME ensure functions production
// calls (ensureSchema / ensureFeedbackSchema / ensureOriginSchema /
// ensureTasksSchema / ensureChatSchema / ensureChatNotesSchema). That is the
// whole point: a new index face joins the reference by existing, and
// TestLedgerUnionRecognizesEveryGopiObject compares this set against a REAL
// built ledger so the two can never drift apart silently.
//
// A reference build that FAILS is an error, never a permissive default: the
// caller then declines to adopt, which is the pre-fix behaviour (refuse the
// file) — the failure direction of a database-shape decision has to be
// "touch nothing".
func gopiLedgerObjectNames(ctx context.Context) (map[string]struct{}, error) {
	ledgerReferenceOnce.Do(func() {
		ledgerReferenceNames, ledgerReferenceErr = buildLedgerReferenceNames(ctx)
	})
	return ledgerReferenceNames, ledgerReferenceErr
}

var (
	ledgerReferenceOnce  sync.Once
	ledgerReferenceNames map[string]struct{}
	ledgerReferenceErr   error
)

// buildLedgerReferenceNames performs the one-time reference build.
func buildLedgerReferenceNames(ctx context.Context) (map[string]struct{}, error) {
	db, err := openDB(ledgerReferenceDBDSN, false)
	if err != nil {
		return nil, fmt.Errorf("ledger reference: %w", err)
	}
	defer db.Close() //stylegate:ignore in-memory reference handle, discarded after the census that read it
	// Every face's schema, in the same order initKnowledgeBase uses, through
	// the SAME functions — a reference assembled from copies of the DDL would
	// be a second source of truth (Rule 14) and would drift.
	for _, ensure := range []func(context.Context, *sql.DB) error{
		ensureSchema, ensureFeedbackSchema, ensureOriginSchema, ensureTasksSchema,
		ensureChatSchema, ensureChatNotesSchema,
		// The two faces whose schema is built LAZILY by their own openers (a
		// procedural memory row, a session-conclusion write) rather than by the
		// first-open build. They are still gopi's objects, and a ledger that
		// already carries one must not read as foreign.
		ensureProceduralSchema, ensureSessionConclusionsSchema,
	} {
		if err := ensure(ctx, db); err != nil {
			return nil, fmt.Errorf("ledger reference: %w", err)
		}
	}
	names, err := readLedgerObjectNames(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("ledger reference: %w", err)
	}
	if len(names) < ledgerReferenceObjectFloor {
		return nil, fmt.Errorf("ledger reference built only %d objects (floor %d) — it did not measure a real schema",
			len(names), ledgerReferenceObjectFloor)
	}
	return names, nil
}

// ledgerReferenceObjectFloor is the smallest object count a full gopi ledger
// schema can plausibly have (the knowledge base alone builds 20+). It exists so
// a reference build that produced NOTHING is an error rather than a set that
// recognizes nothing — the failure would otherwise be silent in the safe
// direction (every file refused) and nobody would notice the census broke.
const ledgerReferenceObjectFloor = 20

// readLedgerObjectNames reads a database's schema object names into a set.
func readLedgerObjectNames(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, ledgerObjectNamesSQL)
	if err != nil {
		return nil, fmt.Errorf("ledger object census: %w", err)
	}
	defer rows.Close()
	names := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ledger object census: %w", err)
		}
		names[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger object census: %w", err)
	}
	return names, nil
}

// ledgerHoldsOnlyGopiObjects reports whether EVERY schema object in the opened
// file is one gopi's own openers mint — i.e. whether this file is gopi's ledger
// in a PARTIAL shape rather than somebody else's database.
//
// The question is intentionally about OBJECTS and not about a table name: an
// empty file is already handled by hasNoSchemaObjects (join its initializer),
// and "carries distilled_qa but not memory_feedback" is already handled by
// openTasksDB's foreign-distilled refusal. This is the third shape, and it is
// the one the chat opener used to leave behind.
//
// An ERROR is returned as an error, never as "not ours": the caller must be
// able to tell "I read the file and it is foreign" from "I could not read the
// file at all" (Rule 15). Neither is an adoption, but only one is information.
// The returned bool is TRUE only for a NON-EMPTY file whose every object is
// gopi's: zero objects is the shape adoptUninitializedKnowledgeBase owns, and
// answering TRUE here would route it through the adoption branch as if it had
// content.
func ledgerHoldsOnlyGopiObjects(ctx context.Context, db *sql.DB) (bool, error) {
	reference, err := gopiLedgerObjectNames(ctx)
	if err != nil {
		return false, err
	}
	names, err := readLedgerObjectNames(ctx, db)
	if err != nil {
		return false, err
	}
	if len(names) == 0 {
		return false, nil
	}
	for name := range names {
		if _, ok := reference[name]; !ok {
			return false, nil
		}
	}
	return true, nil
}

// ledgerOpenMode is the SHAPE an index-face opener asks for: the writing shape
// (build/complete the union, land this face's own objects) or the READ-ONLY
// shape (a report a caller promised not to write).
//
// IT IS A PARAMETER AND NOT A SECOND OPENER because the write has exactly three
// places it can happen — the union build, the face's ensureIndexSchema, and the
// connection's own writability — and a mode switch at the TOP of one function
// keeps all three in one place a reader can audit. A separate read-only
// function would have had to re-state which of the three it skips.
type ledgerOpenMode int

const (
	// ledgerOpenReadWrite is the ordinary shape: `gopi chat backfill` migrating
	// rows, the note index's writer, the live registration hook.
	ledgerOpenReadWrite ledgerOpenMode = iota
	// ledgerOpenReadOnly is: measure, report, and leave the file exactly as it
	// was found. Nothing in this shape may touch the ledger's bytes, its WAL or
	// its -shm — see openLedgerIndexDBReadOnly for the measured reason the
	// writable open cannot be reused behind a query_only pragma.
	ledgerOpenReadOnly
)

// ledgerOpenModeFor maps a caller's read-only REQUEST onto the opener's mode,
// so the mapping lives once (Rule 14) instead of being re-spelled by every
// dry-run face that has to route through it.
func ledgerOpenModeFor(readOnly bool) ledgerOpenMode {
	if readOnly {
		return ledgerOpenReadOnly
	}
	return ledgerOpenReadWrite
}

// ledgerIndexReadCause names the ledger file on a failure that happened AFTER
// the file opened: a corrupted, truncated or unreadable-by-the-driver ledger
// reports "file is not a database (26)" or a schema error, and without this the
// line says what went wrong but not WHICH ledger it happened to. A reader who
// has several ledgers (a sandbox, a copy, the live profile) cannot act on that.
//
// It names the path and nothing else: the driver's own sentence already says
// what is wrong at the file level, and inventing a cause here would be the
// false-diagnosis defect this file's sibling exists to avoid.
func ledgerIndexReadCause(err error, path string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cannot read the ledger index %s: %w", path, err)
}

// ensureLedgerIndexParentDir creates the DIRECTORY that holds the ledger index
// file, through the ONE helper initKnowledgeBase already uses (Rule 14:
// scratch.MkdirAllWithSuiteRunOwner, which claims ownership only for a
// suite-run-named directory it creates and behaves exactly like os.MkdirAll for
// every other one).
//
// It exists because the two faces disagreed about who creates the profile: the
// tasks face reaches initKnowledgeBase and works on an empty GOPI_HOME, while
// this opener called openDB directly, whose first statement creates the FILE and
// therefore needs a DIRECTORY somebody else had to have made. Measured
// 2026-11-06: `GOPI_HOME=<fresh> gopi task list` succeeded and created
// `.gopi/memory.db`, while `gopi session list` on an equally fresh home reported
// a degraded chat index — a difference in the opener, not in the profile.
//
// A path with no directory component (the CWD-relative last resort) is left
// alone: there is nothing to create, and "." must not be re-created.
func ensureLedgerIndexParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return scratch.MkdirAllWithSuiteRunOwner(dir)
}

// openLedgerIndexDB opens the shared ledger for ONE index face, guaranteeing
// that the file it returns is gopi's ledger (the union of the faces) rather
// than this face's private slice of it.
//
// The three shapes, and why each gets its own answer:
//
//   - the file is a knowledge base already — the ordinary case, and the fast
//     path: open it and land this index's own objects (today's behaviour).
//   - the file is ABSENT, or carries ONLY gopi's own objects — the ledger is
//     being created, or was created by a face that predates this function.
//     Build the UNION (ensureKnowledgeBase) and then this index's objects, so
//     no opener can ever again publish the shared file as a non-ledger.
//   - the file carries ANYTHING gopi did not mint — somebody else's database.
//     This opener never builds a knowledge base inside it: it lands only its
//     own objects, exactly as before (the foreign-file behaviour of this face
//     is deliberately unchanged by this fix).
//
// ensureIndexSchema is the face's own ensure function (ensureChatSchema,
// ensureChatNotesSchema), so the union rule is written ONCE and every face
// that shares the file reaches it through this same door (Rule 14).
func openLedgerIndexDB(ctx context.Context, path string, ensureIndexSchema func(context.Context, *sql.DB) error) (*sql.DB, error) {
	return openLedgerIndexDBFor(ctx, path, ledgerOpenReadWrite, ensureIndexSchema)
}

// openLedgerIndexDBReadOnly opens the shared ledger in the shape a caller who
// PROMISED not to write must use — and the promise is kept by the OPEN, not by
// the caller's discipline.
//
// WHY THE MODE HAS TO REACH THIS LAYER (the defect, measured 2026-10-30,
// task 01a188f7). `gopi chat backfill --dry-run` guards each UPDATE on its
// `dryRun` flag, and a copy of the operator's own ledger came back CHANGED
// after ONE dry run anyway: memory.db's sha256 moved (38056aa0… → 0a1135c8…)
// and memory.db-wal was GONE (checkpointed away). The write was never an SQL
// statement — it was the OPEN: openDB(path, false) is a writable connection
// with `_txlock=immediate`, and ensureChatSchema then runs ~8 DDL batches over
// it, so "CREATE TABLE IF NOT EXISTS" (a no-op logically) still opens a write
// transaction.
//
// WHY NOT `openDB(path, true)` — the read-only leg this package already had.
// Measured on a device-backed ledger copy (the probe's log is in the run's
// session directory): openDB(path, true) closed a COPY's -wal and -shm ENTIRELY
// and rewrote the main file (7c08da3c… → 61051ae4…), because its DSN pins
// `_txlock=immediate` for every transaction and its first statement is a
// PRAGMA WRITE (`busy_timeout`). A query_only pragma does not stop the SQLite
// library from checkpointing on close. The DSN-based read-only opener
// (LedgerReadOnlyDSN: mode=ro + query_only) left the main file and the WAL
// byte-identical — only -shm moved, which is the shared wal-index a reader is
// entitled to touch and which carries no ledger data.
//
// So the read-only shape is: no `_txlock=immediate`, no writable DSN, no DDL.
// The file must already exist — a read-only open of a path that is not there
// fails with SQLite's own "unable to open database file", which is the answer
// a dry run of a migration on a nonexistent ledger must give (Rule 15: report
// it, never fall back to the writable shape — that fallback IS the defect).
func openLedgerIndexDBReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	return openLedgerIndexDBFor(ctx, path, ledgerOpenReadOnly, nil)
}

// openLedgerIndexDBFor is the ONE index-face opener; ledgerOpenMode decides
// which of its three write points run.
//
// The read-only arm deliberately skips the whole union question: building or
// completing the union is a WRITE, and a file that is not a gopi ledger is not
// something a dry run may fix. It opens, asserts the read-only guards, and
// hands the handle over.
func openLedgerIndexDBFor(ctx context.Context, path string, mode ledgerOpenMode, ensureIndexSchema func(context.Context, *sql.DB) error) (*sql.DB, error) {
	if mode == ledgerOpenReadOnly {
		if !fileExists(path) {
			// The writable opener would CREATE the file here. A read-only
			// request must not, and must not silently degrade to the write
			// shape that caused the defect it is being asked to avoid.
			return nil, fmt.Errorf("ledger index read-only open: %s does not exist (a read-only caller cannot create it)", path)
		}
		db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
		if err != nil {
			return nil, fmt.Errorf("ledger index read-only open %s: %w", path, err)
		}
		db.SetMaxOpenConns(1) // SQLite serializes; mirror openDB's single connection
		if _, err := db.Exec(ledgerReadQueryOnlyPragma); err != nil {
			db.Close() //stylegate:ignore handle closed after its setup failed; the caller gets the error and no handle
			return nil, fmt.Errorf("ledger index read-only open %s: %w", path, err)
		}
		// ABSENT SCHEMA IS NOT AN ERROR HERE. A dry run of a migration on a
		// ledger whose face was never created has nothing to migrate, and it
		// reports that by finding no rows — the writable opener's
		// CREATE TABLE IF NOT EXISTS was never doing work the caller needed.
		return db, nil
	}
	if !fileExists(path) {
		// Prove the path is OPENABLE before building the union on it. This gate
		// creates a file; it does not create directories — and building the
		// union goes through initKnowledgeBase, which mkdirs the parent, so
		// routing an unopenable path there would turn every broken index path
		// into a silently created database (measured 2026-10-27: cmd/agent's
		// chatResumeTurnBaseline unreadable-index arm went from ERROR to 0,nil).
		// The probe asks the FACT ("did the open succeed?") rather than
		// guessing from the path shape, and a failed probe returns the real
		// open error, which is the answer the caller must hear (Rule 15).
		//
		// THE PROFILE DIRECTORY IS THIS GATE'S JOB, and not creating it was the
		// defect measured 2026-11-06: on a fresh GOPI_HOME (no ~/.gopi yet)
		// `gopi session list` printed
		//
		//	[degraded] the chat index could not be read: unable to open
		//	database file: out of memory (14)
		//
		// because openDB's first statement (PRAGMA busy_timeout) creates the
		// FILE and therefore needs the DIRECTORY, which nothing had made. The
		// sibling face `gopi task list` on the same empty profile worked, and
		// the asymmetry is the whole bug: task list reaches initKnowledgeBase
		// (which mkdirs) while this gate called openDB directly. A profile that
		// does not exist yet is not a broken index — it is a profile with
		// nothing in it — so the directory is created first, through the ONE
		// helper initKnowledgeBase uses (Rule 14), and only then is the open
		// probed. A path that still cannot be opened (a parent that is a file,
		// a permission denial) fails the probe exactly as before, so the guard
		// that arm was added for is untouched.
		if err := ensureLedgerIndexParentDir(path); err != nil {
			return nil, ledgerIndexOpenCause(err, path)
		}
		probe, err := openDB(path, false)
		if err != nil {
			// The driver's text for this failure is "unable to open database
			// file: out of memory (14)" — no path, no cause, and a false
			// allocation diagnosis (see ledger_open_cause.go for the measured
			// reading on an empty profile). Name the file and the real cause.
			return nil, ledgerIndexOpenCause(err, path)
		}
		probe.Close() //stylegate:ignore probe handle closed before its replacement is opened
		return buildLedgerUnion(ctx, path, ensureIndexSchema)
	}
	db, err := openDB(path, false)
	if err != nil {
		return nil, ledgerIndexOpenCause(err, path)
	}
	kb, err := tableExists(ctx, db, knowledgeBaseAnchorTable)
	if err != nil {
		// The FILE opened but the driver could not read it — the shape a
		// corrupted or truncated ledger takes ("file is not a database (26)").
		// The name still belongs on the line: a reader holding several ledgers
		// cannot act on a failure that silently refers to one of them.
		db.Close() //stylegate:ignore probe handle closed after the read that wanted it
		return nil, ledgerIndexReadCause(err, path)
	}
	if kb {
		if err := ensureIndexSchema(ctx, db); err != nil {
			db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
			return nil, ledgerIndexReadCause(err, path)
		}
		return db, nil
	}
	// No distilled_qa. Is this gopi's own file in a PARTIAL shape, or somebody
	// else's? The file answers — and the answer decides between ADOPT (we own
	// it, complete it) and UNCHANGED (we do not own it: touch nothing beyond
	// this face's own tables, exactly as before).
	ownedOnly, censusErr := ledgerHoldsOnlyGopiObjects(ctx, db)
	if censusErr == nil && ownedOnly {
		db.Close() //stylegate:ignore probe handle closed before its replacement is opened
		return buildLedgerUnion(ctx, path, ensureIndexSchema)
	}
	if err := ensureIndexSchema(ctx, db); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, ledgerIndexReadCause(err, path)
	}
	return db, nil
}

// buildLedgerUnion initializes (or completes) the shared ledger as the union
// of its index faces, then lands this face's own objects on the SAME handle.
// ensureKnowledgeBase is the single first-open gate (per-path lock + the
// idempotent build), so two faces racing the same fresh file serialize here
// rather than one of them publishing a partial shape.
//
// It never rebuilds from nothing: every CREATE in the build is IF NOT EXISTS,
// so completing a pre-fix chats-only file keeps the rows that file already
// carries (TestChatOpenerThenTasksOpenerOnAFreshLedger asserts exactly that).
func buildLedgerUnion(ctx context.Context, path string, ensureIndexSchema func(context.Context, *sql.DB) error) (*sql.DB, error) {
	db, err := ensureKnowledgeBase(ctx, path, false)
	if err != nil {
		return nil, err
	}
	if err := ensureIndexSchema(ctx, db); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, err
	}
	return db, nil
}

// ledgerObjectNamesSorted renders a database's object names for a diagnostic
// line or a test assertion. It exists because the boundary this file draws is
// readable only if a failure can say WHICH name was not recognized — "is not a
// distilled knowledge base" alone has cost readers a day of guessing at the
// wrong layer.
func ledgerObjectNamesSorted(ctx context.Context, db *sql.DB) ([]string, error) {
	names, err := readLedgerObjectNames(ctx, db)
	if err != nil {
		return nil, err
	}
	list := make([]string, 0, len(names))
	for name := range names {
		list = append(list, name)
	}
	sort.Strings(list)
	return list, nil
}
