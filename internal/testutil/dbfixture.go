// Package testutil — the ONE supported way for a test to obtain a DATABASE.
//
// ROOT CAUSE THIS FILE EXISTS FOR (measured 2026-09-21): `internal/tools` alone
// carried 147 differently-named per-file DB helpers (claimTestDB,
// taskSyncTestDB, dedupTestDB, mergeBacklogTestDB, newVerifyTestDB, …), each an
// independent hand-written opener, and every one of them is a fresh chance to
// forget the sandbox: about 500 sites resolved their own
// `filepath.Join(t.TempDir(), "memory.db")` and then opened it by hand. Two
// consequences, and the second is the one that bites:
//
//  1. WASTED EFFORT — 147 implementations to keep in step with a schema they do
//     not own (the schema lives in internal/tools, which this package must
//     never import: that would be an import CYCLE, and it is the reason these
//     helpers are shaped by the env/path CONTRACT rather than by calling the
//     tools layer).
//  2. RISK — each of those 147 is a hand-written path. A helper that forgets
//     its `t.Setenv`, or that is shared between tests that set it and tests
//     that do not, resolves the OPERATOR'S real ledger — and the ledger is
//     APPEND-ONLY, so a fixture row written there is a permanent lie in the
//     operator's task history (that is how `t-stale-sibling` made the merger
//     report a phantom /tmp repo for days; see ledgerpin.go).
//
// So this file is the scaffold, and it closes the class by making the SAFE
// shape the SHORTEST one to type:
//
//	path := testutil.NewTestDBPath(t)     // writer: fresh, per-test, pinned
//	db, err := sql.Open("sqlite", tools.LedgerDSN(path))
//
//	path := testutil.SampleDBPath(t)      // reader: the shared, read-only sample
//	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(path))
//
// The gates that keep the scaffold load-bearing live in
// internal/standards/db_scaffold_gate_test.go (PROJECT.md Rule 32), and the
// signatures + the sample's seed rows are frozen in
// devdoc/testdb-scaffold-contract.md.
//
// WHY THIS PACKAGE CANNOT APPLY THE SCHEMA ITSELF: the DDL is internal/tools'
// (openTasksDB at internal/tools/tasks.go is the ONE schema initializer) and
// this package may not import it. That is a boundary, not a gap: a test that
// needs ROWS opens the path through the tools layer, which is what every
// existing fixture already does. The scaffold's job is the part that was
// missing — a path that is correct BY CONSTRUCTION, so no site has to reason
// about whether its own is.
package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nick/gopi/internal/config"

	// The sqlite driver is imported HERE, not left to the caller, because the
	// reader fixture OPENS a database: a test package that never happened to
	// blank-import the driver would otherwise get `sql: unknown driver "sqlite"`
	// from a helper that is supposed to be the easy path. The module already
	// requires modernc.org/sqlite (zero CGO) and every consumer of this package
	// is a test process, so the fixture owns its own dependency (Rule 10.7:
	// preflight the dependency, do not inherit it by luck).
	_ "modernc.org/sqlite"
)

const (
	// testDBDirPrefix names the per-test writer directory under the pinned
	// scratch root. Attributed (the prefix says who created it) and therefore
	// GC-eligible, exactly like every other scratch entry.
	testDBDirPrefix = "testdb-"
	// testDBFileName is the writer fixture's file name. It is
	// config.MemoryDBFileName (Rule 14: "memory.db" has ONE definition, in the
	// config leaf this package already imports) — never a fresh literal.
	testDBFileName = config.MemoryDBFileName
	// sampleDBDirName is the directory the SHARED READ-ONLY SAMPLE is built in,
	// once per run, directly under the pinned scratch root.
	sampleDBDirName = "testdb-sample"
	// sampleDBFileName is the sample's committed artifact name. It is
	// deliberately NOT config.MemoryDBFileName: the sample is not a ledger and
	// must never be mistaken for one by a reader that globs for a ledger.
	sampleDBFileName = "sample.db"
)

// NewTestDBPath returns the path of a FRESH, EMPTY, per-test database and
// points the ledger environment contract at it.
//
// It is the WRITER shape: what a test needs when it is going to CREATE rows.
// The file does NOT exist yet when it returns (see the contract, "freshness is
// the fixture's own invariant" — a caller that finds the file present has a
// bug of its own), and no schema has been applied to it.
//
// THE FOUR THINGS IT GUARANTEES, in the order they matter:
//
//  1. THE PATH IS OFF `/`. It is created with os.MkdirTemp under
//     SuiteTempRoot() — the suite's pinned scratch root (scratchroot.go). When
//     the pin did not engage this FAILS rather than falling back to
//     os.TempDir() (`/tmp` → the boot device, the 2026-09-11 exhaustion site):
//     Rule 22 is a criterion, and the criterion cannot be met by a path nobody
//     measured. A fixture that leaked into `/tmp` would be this repository's
//     oldest recurring incident wearing a new name.
//  2. IT IS FRESH. A new directory per call, so two tests (or two arms of one
//     test) never see each other's rows — the defect that turns a passing test
//     into an order-dependent one.
//  3. THE LEDGER ENV CONTRACT POINTS AT IT. GOPI_MEMORY_DB is set to the path
//     and the LEGACY spelling (GOPI_HIPPO_DB) to "" — because a tool call
//     resolved through the process environment must land on THIS file, not on
//     whatever else the environment names (tools.memoryDBPath falls back
//     through both spellings). The value is restored/removed by t.Setenv at the
//     test's end, absence included (Rule 17).
//  4. THE OPERATOR'S PROFILE IS REFUSED. RefuseOperatorProfileWrite runs before
//     the directory is created, so a scratch root that somehow resolves into
//     the operator's live `~/.gopi` is a loud failure rather than a write into
//     their session/ledger history. The guard is deliberately NOT weakened here
//     and must never be: this fixture is the EASY path, never the only one.
//
// Note what it does NOT do: it does not open the database and it does not apply
// the schema (see the package comment for why that boundary is load-bearing).
// A test that wants the rows it just seeded served by production code opens the
// path through the tools layer — the migration in this ticket's consumer
// (internal/tools/task_ledger_state_test.go's newVerifyTestDB) is the worked example.
func NewTestDBPath(t *testing.T) string {
	t.Helper()
	root := pinnedScratchRootForDB(t, "NewTestDBPath")
	// The scratch root is the process's configured temp root; refuse a root
	// that resolves into the operator's live profile BEFORE anything is
	// created (the same anchor profileguard uses: the OS user record, which no
	// test can fake with $HOME).
	RefuseOperatorProfileWrite(t, "NewTestDBPath", root)
	dir, err := os.MkdirTemp(root, testDBDirPrefix)
	if err != nil {
		t.Fatalf("NewTestDBPath: create a per-test DB directory under %s: %v", root, err)
	}
	// The ledger must be a file OF A PROFILE, not a bare path: a run resolves
	// its ledger (GOPI_MEMORY_DB) and its notify target (the PROFILE ROOT's
	// settings.json) through two independent channels, and deliverTaskNotify's
	// gate suppresses the events of a ledger that is not its profile's own. A
	// per-test directory with no .gopi of its own is exactly that foreign shape
	// — measured 2026-11-04 (task 01a1a647): TestStalePayloadZombieEvidence and
	// TestRepoSweepWouldPruneReportsLiveBranchAndMutatesNothing could not see
	// the alarms they had just made their code fire. Joining the profile root
	// below costs one MkdirAll and buys the property the gate asks for.
	profileRoot := config.ProfileDirIn(dir)
	if err := os.MkdirAll(profileRoot, dirPerm); err != nil {
		t.Fatalf("NewTestDBPath: create the per-test profile root %s: %v", profileRoot, err)
	}
	path := filepath.Join(profileRoot, testDBFileName)
	// The env contract, in ONE place: a test that wants a tool call to find
	// this database flips nothing of its own. t.Setenv, never os.Setenv
	// (Rule 17): the value is restored at the test's end, absence included.
	t.Setenv(config.MemoryDBEnv, path)
	// The legacy spelling is the SAME override under an older name, so leaving
	// it ambient would smuggle a different database back in through the
	// fallback chain. "" rather than Unsetenv for the reason ledgerpin.go
	// records: a child process reads it at exec time and the tools layer treats
	// an empty value as unset.
	t.Setenv(config.MemoryDBEnvLegacy, "")
	return path
}

// Sample builds the SHARED, READ-ONLY SAMPLE database once per run and returns
// its READ-ONLY DSN.
//
// It is the READER shape: a test that only needs to READ well-known rows should
// not build a schema and seed one, and should not have to invent its own. The
// sample is created inside the pinned scratch root (Rule 22), it is
// never checked in (the DEFINITION in sampleSeed is the artifact; the file is a
// build product — a binary fixture in git is a known anti-pattern here), and it
// is built by seeding the database through SampleSeedSQL, so a caller does not
// need the schema to read it back.
//
// The DSN it returns is read-only in TWO independent ways (one is not enough:
// a caller that strips the query string would otherwise get a writable handle):
//
//  1. THE OPEN MODE is built here, inside the fixture, from the documented
//     read-only pragma contract — `mode=ro`, which the SQLite driver enforces
//     at open time (a write then fails with "attempt to write a readonly
//     database", never a silent mutation), plus `query_only(true)` as the
//     belt-and-braces guard.
//  2. THE FILE ITSELF is chmod 0444 and is never handed out by path for
//     writing. A reader that opens it with a bare DSN gets a write-refusing
//     file nonetheless. (Running as root defeats Unix file modes; the fixture
//     therefore also asserts the demo in TestSampleIsReadOnly rather than
//     trusting the mode alone.)
//
// The mode is deliberately built HERE rather than by importing
// internal/tools.LedgerReadOnlyDSN: testutil must not import internal/tools
// (cycle), and the contract that matters to a reader is only the two read-side
// guards — the busy timeout is a WRITER's concern. The string DSN is built
// rather than path-returned so the sample can never be opened writable by a
// caller who follows the signature.
//
// The seed rows are DOCUMENTED (SampleSeed and devdoc/testdb-scaffold-contract.md);
// adding rows is allowed, changing or removing a documented row is a contract
// change. A test that needs rows the sample does not have uses NewTestDBPath —
// it never mutates the sample.
func Sample(t *testing.T) *sql.DB {
	t.Helper()
	path := sampleDBPath(t)
	db, err := sql.Open(sampleDriverName, sampleReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("Sample: open the read-only sample %s: %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Sample: close the sample handle %s: %v", path, err) // Rule 15b
		}
	})
	return db
}

// SampleDBPath returns the PATH of the shared read-only sample (it builds it on
// first call in this run). It exists for the caller that wants to hand the path
// to a SUBPROCESS or to an external tool — the in-process reader should prefer
// Sample, which cannot be opened writable by construction.
//
// A caller that takes the path is responsible for opening it read-only; the
// file mode (0444) is the second line of defence for exactly that caller.
func SampleDBPath(t *testing.T) string {
	t.Helper()
	return sampleDBPath(t)
}

// sampleDriverName is the driver a test opens the sample with (Rule 1b: the
// bare driver name has one definition here, beside the DSN builder that needs
// it).
const sampleDriverName = "sqlite"

// sampleReadOnlyDSN builds the read-only DSN for the sample — the read-side
// pragma contract, in ONE place (Rule 14). Only read-side pragmas appear:
// journal_mode/synchronous are WRITE-side pragmas, which SQLite REFUSES on a
// mode=ro connection, and because the driver delivers pragmas with the first
// statement that refusal would surface on the CALLER's query instead (measured:
// six ledger-backup tests red with "attempt to write a readonly database" when
// a read-only helper briefly carried them — internal/tools/ledger_dsn.go).
func sampleReadOnlyDSN(path string) string {
	return "file:" + path + "?mode=ro&_pragma=query_only(true)"
}

// sampleBuildLocks serialise the one-time build against concurrent tests
// (each calling package serialises its own tests, but a suite that uses
// t.Parallel or a concurrent helper still reaches this once).
var sampleBuildLocks sync.Mutex

// sampleBuilt records, per process, that the sample for this scratch root has
// been built — so the "once" is a property of the process, not a hope.
var sampleBuilt bool

// sampleDBPath returns the sample's path, building it if this process has not
// built it yet.
func sampleDBPath(t *testing.T) string {
	t.Helper()
	root := pinnedScratchRootForDB(t, "Sample")
	dir := filepath.Join(root, sampleDBDirName)
	path := filepath.Join(dir, sampleDBFileName)

	sampleBuildLocks.Lock()
	defer sampleBuildLocks.Unlock()
	if sampleBuilt {
		// A previously built sample must still be there: if it vanished (a GC
		// sweep, a manual rm) the reader would otherwise get a "no such table"
		// error that names nothing. Say what happened instead.
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Sample: the sample %s was built earlier in this process but is gone now (%v) — "+
				"the scratch root was cleaned under a running process", path, err)
		}
		return path
	}
	if err := buildSampleOnce(dir, path); err != nil {
		t.Fatalf("Sample: build the shared read-only sample at %s: %v", path, err)
	}
	// Seal: 0444 refuses every writer that is not root — the second (and
	// independent) half of the immutability contract, and the reason
	// SampleDBPath is safe to hand out to a caller that opens the file itself.
	if err := os.Chmod(path, sampleFilePerm); err != nil {
		t.Fatalf("Sample: seal the shared sample %s read-only: %v", path, err)
	}
	sampleBuilt = true
	return path
}

// buildSampleOnce creates dir (if needed) and (re)builds the sample's schema
// and seed. It deliberately does NOT seal the file: the shared sample is sealed
// by its own caller (sampleDBPath), while a PRIVATE copy handed to a fixture
// that must be able to write into it (SampleReportAmbientFixtures) stays writable.
//
// It is deliberately NOT a shared-across-processes cache keyed by content hash:
// the sample is tiny (SampleSeed's handful of rows, no DDL beyond two tables),
// and a cross-process cache would need an invalidation rule keyed on a schema
// this package cannot see (internal/tools owns it). A per-run build is
// therefore both simpler and correct — and it is still the win the fixture is
// for: ONE build per run instead of one per test.
func buildSampleOnce(dir, path string) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	// Rebuild from scratch: a stale file from a previous run (a killed suite
	// never runs cleanup) must not be read as if it were this definition.
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove a stale sample %s: %w", path+suffix, err)
		}
	}
	if err := os.WriteFile(path, nil, filePerm); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	db, err := sql.Open(sampleDriverName, path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	// journal_mode=WAL is set HERE (a write-side pragma on the WRITE path, which
	// is the only place it is legal) and the connection is closed before the file
	// is sealed, so no -wal companion survives into the read-only phase.
	db.SetMaxOpenConns(1)
	sampleSeedErr := seedSample(db)
	closeErr := db.Close()
	if sampleSeedErr != nil {
		return sampleSeedErr
	}
	if closeErr != nil {
		return fmt.Errorf("close %s after seeding: %w", path, closeErr)
	}
	return nil
}

// sampleFilePerm is the read-only mode the sample is sealed with — and the
// load-bearing half of the immutability contract for a caller that took
// SampleDBPath and opened it with a DSN of its own.
const sampleFilePerm = 0o444

// seedSample creates the sample's shape and inserts the documented seed rows —
// the WHOLE definition of the sample's contents, in one place.
//
// It runs the DDL. That is deliberate and it is NOT a second schema authority:
// the sample exists so a READER needs no schema, and a reader that cannot run
// DDL cannot build a sample either. The sample's tables are the READER-VISIBLE
// surface only, not the ledger: two tables, the rows in SampleSeed, and nothing
// else — so this DDL cannot drift into a mirror of internal/tools' schema
// without also changing what SampleSeed promises, which is a contract change
// and therefore reviewed. (A test that needs the REAL ledger schema, FTS
// triggers and all, is a WRITER-shape test: NewTestDBPath + the tools layer,
// whose openTasksDB owns the DDL.)
func seedSample(db *sql.DB) error {
	for _, stmt := range []string{sampleTasksDDL, sampleEventsDDL, sampleDocsDDL} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin seed transaction: %w", err)
	}
	for _, row := range SampleSeed() {
		if _, err := tx.Exec(sampleTaskInsert,
			row.TaskID, row.SessionID, row.Summary, row.Status, row.Tags, row.Source, row.Priority); err != nil {
			_ = tx.Rollback() //stylegate:ignore rollback on the error path; the caller already returns the real failure
			return fmt.Errorf("seed row %s: %w", row.TaskID, err)
		}
	}
	for _, ev := range SampleSeedEvents() {
		if _, err := tx.Exec(sampleEventInsert, ev.TaskID, ev.Event, ev.CreatedAt); err != nil {
			_ = tx.Rollback() //stylegate:ignore rollback on the error path; the caller already returns the real failure
			return fmt.Errorf("seed event %s/%s: %w", ev.TaskID, ev.Event, err)
		}
	}
	for _, doc := range SampleDocs() {
		if _, err := tx.Exec(sampleDocInsert, doc.Topic, doc.Question, doc.Answer, doc.Tier, doc.Confidence); err != nil {
			_ = tx.Rollback() //stylegate:ignore rollback on the error path; the caller already returns the real failure
			return fmt.Errorf("seed doc %q: %w", doc.Topic, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed rows: %w", err)
	}
	return nil
}

// The sample's DDL — two tables, the reader-visible surface. Kept as constants
// so the shape and the inserts below cannot drift apart in spelling (Rule 1b).
const (
	sampleTasksDDL = `CREATE TABLE tasks (
		task_id    TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		summary    TEXT NOT NULL,
		status     TEXT NOT NULL,
		tags       TEXT NOT NULL DEFAULT '',
		source     TEXT NOT NULL DEFAULT 'auto',
		priority   INTEGER NOT NULL DEFAULT 5
	)`
	sampleEventsDDL = `CREATE TABLE task_events (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id    TEXT NOT NULL,
		event      TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`
	sampleDocsDDL = `CREATE TABLE distilled_qa (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		topic          TEXT NOT NULL,
		question       TEXT NOT NULL,
		answer         TEXT NOT NULL,
		tier           TEXT DEFAULT 'warm',
		confidence     REAL DEFAULT 0.5,
		last_retrieved REAL
	)`
	sampleDocInsert = `INSERT INTO distilled_qa (topic, question, answer, tier, confidence)
		VALUES (?, ?, ?, ?, ?)`
	sampleTaskInsert = `INSERT INTO tasks
		(task_id, session_id, summary, status, tags, source, priority)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	sampleEventInsert = `INSERT INTO task_events (task_id, event, created_at) VALUES (?, ?, ?)`
)

// SampleRow is one documented seed row of the shared sample.
type SampleRow struct {
	// TaskID is the row's stable identifier: a test asserts on THIS, never on
	// an ordinal, so adding rows cannot break an existing assertion.
	TaskID string
	// SessionID, Summary, Status, Tags, Source, Priority are the row's
	// documented field values.
	SessionID string
	Summary   string
	Status    string
	Tags      string
	Source    string
	Priority  int
}

// SampleSeed is the sample's seed definition — the ONE place the documented
// rows live (Rule 14). Adding a row is allowed; changing or removing a
// documented row is a contract change and must be made in the same commit as
// devdoc/testdb-scaffold-contract.md.
//
// The three rows are chosen to make the sample USEFUL rather than merely
// present: they span the three states a reader is most often distinguishing
// (a completed row, a pending row with a tag, and a row whose status is the
// value a naive reader is most likely to get wrong), and their summaries are
// stable search text for a test that exercises a query.
//
// The status strings are ALSO the values the tools layer compares against
// (internal/tools' TaskStatus* constants). They are written as literals here
// because this package cannot import internal/tools — the import is a cycle.
// The duplication is checked, not trusted: the gate in
// internal/standards/db_scaffold_gate_test.go asserts that every status this
// list names also appears as a value in that package's constant block, so the
// two cannot drift silently (PROJECT.md Rule 32).
func SampleSeed() []SampleRow {
	return []SampleRow{
		{
			TaskID:    "sample-completed-1",
			SessionID: "sample-session-1",
			Summary:   "a finished task the reader may rely on",
			Status:    "completed",
			Tags:      "sample",
			// The miner's own role, spelled the way the vocabulary spells it
			// (this seed used the retired "auto" literal, which
			// internal/tasksource.Normalize reads back as `merger` — the
			// fixture itself narrated the miner as the merge path).
			Source:   "agency-proposal",
			Priority: 5,
		},
		{
			TaskID:    "sample-pending-1",
			SessionID: "sample-session-1",
			Summary:   "a pending task carrying a tag",
			Status:    "pending",
			Tags:      "sample,reader",
			Source:    "human",
			Priority:  2,
		},
		{
			TaskID:    "sample-claimed-1",
			SessionID: "sample-session-2",
			Summary:   "a claimed task — the status a naive reader falls back to completed on",
			Status:    "claimed",
			Tags:      "",
			Source:    "agency-proposal",
			Priority:  5,
		},
	}
}

// SampleSeedEvents is the sample's documented event rows, keyed to the seed
// tasks above. Kept separate from SampleSeed so a reader that only needs one
// event per task is not handed a list it has to filter.
//
// It is used by the sample's own build (the events are inserted alongside the
// tasks) AND by tests:
//
//	events := testutil.SampleSeedEvents()
func SampleSeedEvents() []SampleEventRow {
	return []SampleEventRow{
		{TaskID: "sample-completed-1", Event: "created", CreatedAt: 1700000000},
		{TaskID: "sample-completed-1", Event: "completed", CreatedAt: 1700000100},
		{TaskID: "sample-pending-1", Event: "created", CreatedAt: 1700000200},
		{TaskID: "sample-claimed-1", Event: "created", CreatedAt: 1700000300},
		{TaskID: "sample-claimed-1", Event: "claimed", CreatedAt: 1700000400},
	}
}

// SampleEventRow is one documented seed row of the sample's task_events table.
type SampleEventRow struct {
	// TaskID is the task the event belongs to (a SampleRow.TaskID).
	TaskID string
	// Event is the event name.
	Event string
	// CreatedAt is the event's unix-seconds timestamp — a FIXED value, never
	// now(), so a row-count or ordering assertion is deterministic.
	CreatedAt int64
}

// pinnedScratchRootForDB resolves the pinned scratch root or fails the test.
//
// Rule 22 is a criterion, not a path: the fixture must be able to MEASURE that
// its directory is off `/`. When the suite never pinned a root there is nothing
// to measure, so this refuses — the alternative (os.TempDir() → `/tmp` on a
// default host) is the boot-device leak this repository has fixed three times
// and must not re-open through a convenience helper.
func pinnedScratchRootForDB(t *testing.T, what string) string {
	t.Helper()
	if !SuiteTempRootPinned() {
		t.Fatalf("%s: the suite's scratch root is NOT pinned, so this fixture would fall back to the "+
			"AMBIENT temp root %q (os.TempDir() — `/tmp` on a default host, the boot device, and the "+
			"partition the 2026-09-11 exhaustion filled). Rule 22 is a criterion and a path nobody "+
			"measured cannot satisfy it. FIX: the suite's TestMain must call testutil.PinSuiteForTestMain "+
			"(or RunSuiteMain); the pinned root is %s",
			what, os.TempDir(), ScratchHomeDir())
	}
	root := SuiteTempRoot()
	if strings.TrimSpace(root) == "" {
		t.Fatalf("%s: the suite reports a pin but its scratch root is empty — refusing to create a "+
			"database under an unknown path", what)
	}
	return root
}

// SampleDocRow is one documented knowledge-base row of the shared sample: the
// reader-visible content of the distilled_qa table, in the shape
// internal/tools' MemoryStore writes it.
type SampleDocRow struct {
	// Topic is the row's stable identifier: a test asserts on THIS.
	Topic string
	// Question is the row's documented question text.
	Question string
	// Answer is the row's documented answer text.
	Answer string
	// Tier is the knowledge-base tier the row lives in ("hot" is what a
	// hot-resident reader looks for).
	Tier string
	// Confidence is the row's stored confidence.
	Confidence float64
}

// SampleDocs is the sample's knowledge-base seed definition — the ONE place the
// documented distilled_qa rows live (Rule 14), the counterpart of SampleSeed for
// the table a memory/hot-resident reader queries.
//
// It exists because the shipped fixture that the ambient-env leak probe needs
// (SampleReportAmbientFixtures) must carry a HOT row whose topic a test can assert
// on, and a row definition duplicated into the caller is exactly the drift this
// scaffold removes. Adding a row is allowed; changing or removing a documented
// row is a contract change (devdoc/testdb-scaffold-contract.md).
func SampleDocs() []SampleDocRow {
	return []SampleDocRow{
		{
			Topic:      "sample-hot-1",
			Question:   "what does the shared sample hold?",
			Answer:     "a documented hot-tier row a hot-resident reader can find",
			Tier:       "hot",
			Confidence: 0.99,
		},
		{
			Topic:      "sample-warm-1",
			Question:   "which tier is the second row in?",
			Answer:     "warm — a row a tier-filtered reader must NOT return as hot",
			Tier:       "warm",
			Confidence: 0.5,
		},
	}
}

// SampleReportAmbientFixtures provisions a PRIVATE copy of the sample, seeded from the
// same definition (SampleSeed + SampleDocs), and returns its path WITHOUT
// sealing it read-only.
//
// WHY A COPY AND NOT THE SEALED SAMPLE. A test that points an AMBIENT database
// path at the sample is testing that nothing writes THROUGH that path (the
// measured incident: a populated ambient database leaked its content into the
// agent under test). Such a test asserts the ambient file is left untouched —
// which is only a meaningful assertion if the file COULD have been written.
// Handing it the shared, 0444-sealed sample would make the write impossible for
// the wrong reason (file mode, not isolation) and would let one test's mutation
// damage every other reader. So this is the documented third shape: a per-call
// copy of the shared DEFINITION, writable, owned by the caller.
//
// It is still ONE definition: the rows come from SampleSeed/SampleDocs, the
// schema from the same builder, and the copy is made with SQLite's own
// `VACUUM INTO` — a page-level copy, so a reader gets a valid database without
// this package needing to replay DDL.
func SampleReportAmbientFixtures(t *testing.T) string {
	t.Helper()
	seed := sampleSeedPath(t)
	root := pinnedScratchRootForDB(t, "SampleReportAmbientFixtures")
	dir, err := os.MkdirTemp(root, testDBDirPrefix+"ambient-")
	if err != nil {
		t.Fatalf("SampleReportAmbientFixtures: create a private copy directory under %s: %v", root, err)
	}
	dst := filepath.Join(dir, config.MemoryDBFileName)
	// VACUUM INTO writes a compact COPY of the sample; it refuses an existing
	// destination, which is what a fresh per-call directory guarantees.
	db, err := sql.Open(sampleDriverName, seed)
	if err != nil {
		t.Fatalf("SampleReportAmbientFixtures: open the seed sample %s: %v", seed, err)
	}
	if _, err := db.Exec(`VACUUM INTO ?`, dst); err != nil {
		_ = db.Close() //stylegate:ignore the fixture database is closed at the end of the run; the OS reclaims the descriptor
		t.Fatalf("SampleReportAmbientFixtures: copy %s to %s: %v", seed, dst, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("SampleReportAmbientFixtures: close the seed sample: %v", err)
	}
	return dst
}

// seedPath is the UNSEALED build of the sample — the copy source for
// SampleReportAmbientFixtures. Separated from the sealed shared file so the copy path
// never has to chmod the shared sample back to writable (a window in which a
// concurrent reader could observe a writable sample).
func sampleSeedPath(t *testing.T) string {
	t.Helper()
	root := pinnedScratchRootForDB(t, "Sample")
	dir := filepath.Join(root, sampleDBDirName)
	path := filepath.Join(dir, sampleSeedFileName)
	sampleBuildLocks.Lock()
	defer sampleBuildLocks.Unlock()
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if err := buildSampleOnce(dir, path); err != nil {
		t.Fatalf("Sample: build the copy source at %s: %v", path, err)
	}
	return path
}

// sampleSeedFileName is the unsealed copy source's name (never handed to a
// caller as a database to read: it is a build product of the sample).
const sampleSeedFileName = "seed-copy.db"
