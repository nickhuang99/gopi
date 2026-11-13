package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
)

// tempMemoryDB points the memory DB at a throwaway file for the test.
func tempMemoryDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-gopi.db")
	t.Setenv(memoryDBEnv, path)
	return path
}

func TestWriteTaskArea_RequiresConclusionAndCompleted(t *testing.T) {
	tempMemoryDB(t)
	cases := []struct {
		name    string
		opts    WriteTaskAreaOptions
		wantErr string
	}{
		{"empty session", WriteTaskAreaOptions{SessionID: "", TaskID: "1", Status: "completed", Conclusion: "x"}, "session_id"},
		{"empty task", WriteTaskAreaOptions{SessionID: "s1", TaskID: "", Status: "completed", Conclusion: "x"}, "task_id"},
		{"bad status", WriteTaskAreaOptions{SessionID: "s1", TaskID: "1", Status: "wip", Conclusion: "x"}, "status"},
		{"empty conclusion", WriteTaskAreaOptions{SessionID: "s1", TaskID: "1", Status: "completed", Conclusion: ""}, "conclusion"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := WriteTaskArea(tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("WriteTaskArea(%+v) err = %v, want containing %q", tc.opts, err, tc.wantErr)
			}
		})
	}
}

func TestWriteTaskArea_RecordsAppendOnlyScoped(t *testing.T) {
	tempMemoryDB(t)
	if _, err := WriteTaskArea(WriteTaskAreaOptions{SessionID: "sessA", TaskID: "1", Status: "completed", Conclusion: "item1 done: the sky is blue"}); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if _, err := WriteTaskArea(WriteTaskAreaOptions{SessionID: "sessA", TaskID: "2", Status: "completed", Conclusion: "item2 done: cats are liquid", CompressedAnalysis: "compressible reasoning"}); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	// Another session must not see sessA's conclusions.
	if _, err := WriteTaskArea(WriteTaskAreaOptions{SessionID: "sessB", TaskID: "1", Status: "completed", Conclusion: "other session item"}); err != nil {
		t.Fatalf("write B: %v", err)
	}

	got, err := SessionConclusions("sessA")
	if err != nil {
		t.Fatalf("SessionConclusions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d conclusions for sessA, want 2", len(got))
	}
	if got[0].TaskID != "1" || !strings.Contains(got[0].Conclusion, "sky is blue") {
		t.Errorf("conclusion[0] = %+v", got[0])
	}
	if got[1].TaskID != "2" || got[1].Analysis != "compressible reasoning" {
		t.Errorf("conclusion[1] = %+v (analysis should persist when provided)", got[1])
	}

	other, err := SessionConclusions("sessB")
	if err != nil {
		t.Fatalf("SessionConclusions B: %v", err)
	}
	if len(other) != 1 || !strings.Contains(other[0].Conclusion, "other session") {
		t.Errorf("sessB conclusions = %+v", other)
	}

	empty, err := SessionConclusions("never-wrote")
	if err != nil {
		t.Fatalf("SessionConclusions never-wrote: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("never-wrote conclusions = %+v, want none", empty)
	}
}

func TestSessionConclusions_MissingDBReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(memoryDBEnv, filepath.Join(dir, "does-not-exist.db"))
	got, err := SessionConclusions("any-session")
	if err != nil {
		t.Fatalf("missing DB must read as empty, got err %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing DB conclusions = %+v, want none", got)
	}
}

// The DB lives under the test env override; ensure we never write to the
// real gopi DB when tests run.
func TestWriteTaskArea_DoesNotTouchRealDB(t *testing.T) {
	realPath := os.Getenv(memoryDBEnv)
	tempMemoryDB(t)
	if _, err := WriteTaskArea(WriteTaskAreaOptions{SessionID: "s", TaskID: "1", Status: "completed", Conclusion: "x"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if os.Getenv(memoryDBEnv) == realPath && realPath != "" {
		t.Fatal("test wrote to the real memory DB")
	}
}

// ---------------------------------------------------------------------------
// Regression coverage for the cold-DB bootstrap budget
// (devdoc/write-task-area-schema-timeout.md).
//
// The flake TestT2_E3_ResumeCarriesForwardCommittedConclusions failed with
// "write_task_area: schema: context deadline exceeded" because ONE 5s deadline
// covered both the bootstrap (create file + full knowledge-base schema init +
// WAL close sync) and the session-scoped insert. The tests below pin the two
// halves of that diagnosis: the budgets are distinct constants with the
// bootstrap the larger of the two, and an expired bootstrap is REPORTED AS a
// budget expiry rather than surfacing as a schema/DB refusal.
// ---------------------------------------------------------------------------

// TestWriteTaskAreaBootstrapBudgetExceedsQueryBudget pins the relationship the
// fix rests on. A regression that collapses the two budgets back into one
// (taskAreaWriteTimeout raised to cover the insert, or the bootstrap lowered
// to the query budget) makes this red — that collapse IS the flake.
func TestWriteTaskAreaBootstrapBudgetExceedsQueryBudget(t *testing.T) {
	if taskAreaWriteTimeout <= taskAreaDBTimeout {
		t.Fatalf("bootstrap budget %v must exceed the session-query budget %v — one shared deadline is the flake "+
			"(the cold-DB schema init does not fit the budget sized for a scoped query)",
			taskAreaWriteTimeout, taskAreaDBTimeout)
	}
	t.Setenv(bootstrapTimeoutEnvName, "")
	if got := taskAreaWriteBootstrapTimeout(); got != taskAreaWriteTimeout {
		t.Fatalf("unset %s gave budget %v, want the default %v", bootstrapTimeoutEnvName, got, taskAreaWriteTimeout)
	}
}

// TestWriteTaskAreaBootstrapBudgetEnvOverride — the operator-flippable knob is
// read in exactly one place and a non-positive value is ignored (a zero budget
// would fire immediately and turn a typo into a hard failure of every commit).
func TestWriteTaskAreaBootstrapBudgetEnvOverride(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"90", 90 * time.Second},
		{" 45 ", 45 * time.Second},
		{"0", taskAreaWriteTimeout},
		{"-3", taskAreaWriteTimeout},
		{"not-a-number", taskAreaWriteTimeout},
	} {
		t.Setenv(bootstrapTimeoutEnvName, tc.raw)
		if got := taskAreaWriteBootstrapTimeout(); got != tc.want {
			t.Errorf("%s=%q gave budget %v, want %v", bootstrapTimeoutEnvName, tc.raw, got, tc.want)
		}
	}
}

// TestWriteTaskAreaBootstrapExpiryIsReportedAsABudget pins the error
// CLASSIFICATION (no timing, no parked SQLite connection): an expired budget
// says so, names the constant and the budget — while an open failure on a
// live context passes through untouched, because a foreign/read-only DB is a
// real refusal, not a timeout. This is the difference the old code could not
// express (both came out as a bare "context deadline exceeded").
func TestWriteTaskAreaBootstrapExpiryIsReportedAsABudget(t *testing.T) {
	expired, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-expired.Done() // the budget is spent before the classification runs

	openErr := errors.New("file is not a database")
	got := classifyBootstrapFailure(expired, taskAreaWriteTimeout, openErr)
	if errors.Is(got, openErr) {
		t.Errorf("an expired budget re-reported the raw open error %v — the caller cannot tell a slow environment from a refusing DB", got)
	}
	for _, want := range []string{taskAreaWriteTimeout.String(), bootstrapTimeoutEnvName, "schema init"} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("budget-expiry error %q does not name %q", got, want)
		}
	}

	live, cancelLive := context.WithTimeout(context.Background(), time.Minute)
	defer cancelLive()
	if got := classifyBootstrapFailure(live, taskAreaWriteTimeout, openErr); !errors.Is(got, openErr) {
		t.Errorf("a live budget wrapped the open error into %v, want the original refusal %v", got, openErr)
	}
}

// TestWriteTaskAreaBootstrapBudgetIsEnvWidened proves the knob actually reaches
// the bootstrap (not just the resolver): the documented failure recipe is
// "widen the knob", so the knob must be read on the real call path.
func TestWriteTaskAreaBootstrapBudgetIsEnvWidened(t *testing.T) {
	tempMemoryDB(t)
	t.Setenv(bootstrapTimeoutEnvName, "120")
	read := make(chan time.Duration, 1)
	orig := bootstrapTimeoutProbe
	bootstrapTimeoutProbe = func(d time.Duration) { read <- d }
	defer func() { bootstrapTimeoutProbe = orig }()

	if _, err := WriteTaskArea(WriteTaskAreaOptions{SessionID: "s", TaskID: "1", Status: "completed", Conclusion: "c"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case got := <-read:
		if got != 120*time.Second {
			t.Fatalf("bootstrap ran with budget %v, want the %s override 120s", got, bootstrapTimeoutEnvName)
		}
	default:
		t.Fatal("the bootstrap budget was never resolved on the write path")
	}
}

// TestWriteTaskAreaBootstrapCoversTheSchemaHeal pins the cost-class boundary
// the flake's second half exposed: ensureSessionConclusionsSchema creates and
// indexes a table on a COLD DB (DDL, each statement fsynced at the WAL-close
// checkpoint) and therefore belongs INSIDE the bootstrap budget. Bounding it
// with the short query budget reproduces the original symptom verbatim
// ("write_task_area: schema: context deadline exceeded") on an unpinned
// boot-device scratch root under I/O load.
//
// The probe is deterministic and timing-free: a second connection parks a
// write lock, so the heal's DDL cannot proceed (dbBusyTimeoutMSec = 15s ≫ the
// budgets below). With the bootstrap budget at 1s, the heal can only ever end
// one of two ways:
//   - it inherits bootstrapWriteAreaDB's ctx → named budget expiry (what the
//     fix guarantees), or
//   - it runs on a FRESH taskAreaDBTimeout (5s) ctx → a bare
//     "schema: context deadline exceeded", the raw pre-fix symptom.
//
// The second case makes this test red, which is the point: moving the heal out
// of the bootstrap boundary cannot pass silently.
func TestWriteTaskAreaBootstrapCoversTheSchemaHeal(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	path := tempMemoryDB(t)
	t.Setenv(bootstrapTimeoutEnvName, "1")

	// Initialize the DB once, then DROP the working-area table: the heal must
	// actually CREATE (a CREATE TABLE IF NOT EXISTS on an existing table is a
	// no-op that needs no write lock), which is the cold-DB cost class this
	// test is about.
	seed, err := bootstrapWriteAreaDB()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := seed.Exec(`DROP TABLE session_conclusions`); err != nil {
		seed.Close() //stylegate:ignore test cleanup on the t.Fatalf error path after a failed DROP TABLE; the test is already failing, so a close error adds nothing
		t.Fatalf("drop: %v", err)
	}
	seed.Close() //stylegate:ignore test closes the seed handle before the write-area path opens the DB; a close error would surface as the open failure asserted next

	parker, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open parker: %v", err)
	}
	defer parker.Close()
	tx, err := parker.Begin() // BEGIN IMMEDIATE via _txlock (openDB)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	//stylegate:ignore rollback on the error path; the original failure is already being returned
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE wta_lock_probe (x INTEGER)`); err != nil {
		t.Fatalf("lock: %v", err)
	}

	start := time.Now()
	_, err = WriteTaskArea(WriteTaskAreaOptions{SessionID: "s", TaskID: "1", Status: "completed", Conclusion: "c"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("the write SUCCEEDED while another connection held the write lock — the schema heal never ran")
	}
	msg := err.Error()
	if strings.Contains(msg, "write_task_area: schema:") {
		t.Fatalf("the heal failed on its own short budget after %v: %v — ensureSessionConclusionsSchema must run on bootstrapWriteAreaDB's ctx (the bootstrap budget), not a fresh taskAreaDBTimeout one", elapsed, err)
	}
	// The heal ran under the bootstrap budget, so the blocked DDL is reported
	// as a bootstrap budget expiry naming its knob (the insert's own attributed
	// message is the alternative if the table happened to exist already) —
	// never a bare "context deadline exceeded" (Rule 15).
	if !strings.Contains(msg, bootstrapTimeoutEnvName) && !strings.Contains(msg, "insert") {
		t.Fatalf("blocked-write failure %v (after %v) names no step and no budget", msg, elapsed)
	}
	// Upper bound: the DEPTH of the block is SQLite's busy_timeout
	// (dbBusyTimeoutMSec), which outlasts either budget — the budgets bound the
	// WAIT the caller observes, the busy wait bounds the stall itself. Asserting
	// the tighter promise (elapsed <= bootstrap budget) would be a lie about
	// the driver; see devdoc/write-task-area-schema-timeout.md.
	if elapsed > time.Duration(dbBusyTimeoutMSec)*time.Millisecond+time.Second {
		t.Fatalf("blocked write took %v, beyond even SQLite's %dms busy wait — the write path is stalling unboundedly",
			elapsed, dbBusyTimeoutMSec)
	}
}
