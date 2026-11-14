package tools

// task_fixture_reclaim_test.go — the two halves of the fixture-row reclaim,
// driven through the REAL surfaces (TaskSubmit, FixtureReclaimSweep, TaskShow)
// rather than the predicates in isolation.
//
// WHAT EACH TEST PROVES, and the failure it would catch:
//
//	① creator readable   — a row filed by a RUN records that run's session id,
//	                        both in the ledger column and in the session's
//	                        metadata.json. Before the stamp, BOTH were empty:
//	                        `task show` rendered `LLM session — (task_submit)`.
//	② reclaim fires      — a pending fixture row whose creator's run row is
//	                        TERMINAL becomes cancelled, with the creator and the
//	                        evidence in its reason.
//	③ live creator kept  — a fixture row filed by a row still in progress stays
//	                        pending. This is the direction that must never
//	                        regress: reaping a probe in active use is the one
//	                        outcome the arm is forbidden to produce.
//	④ unknown kept       — a fixture row whose creator this ledger has no record
//	                        of stays pending, WITH a stated reason (Rule 15: "I
//	                        could not tell" is never "abandoned").

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tasktags"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// fixtureReclaimLedger pins a fresh per-test ledger AND a private profile root
// (Rule 32: the scaffold owns the path and the environment; Rule 34: never the
// operator's live profile).
func fixtureReclaimLedger(t *testing.T) string {
	t.Helper()
	db := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", db)
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	return db
}

// submitFixtureAs files a fixture row naming `creator` as the session that
// filed it — through the ordinary submit path (the reserved tag is what makes
// it a fixture, never ledger surgery).
func submitFixtureAs(t *testing.T, creator, summary string) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:   summary,
		Payload:   "seeded fixture: " + summary,
		Tags:      tasktags.FixtureTag,
		Source:    "agent",
		SessionID: creator,
	})
	if err != nil {
		t.Fatalf("submit fixture %s: %v", summary, err)
	}
	return taskIDFromSubmitOutput(t, out)
}

// seedCreatorRun writes the RUN row a creator session would own, with `status`,
// so the reclaim's ledger arm has something real to read.
//
// THE SESSION_ID IS DELIBERATELY EMPTY, because that is what a real run row
// looks like when it is submitted the ordinary way: a worker's session id IS
// its task id (the claim writes the same string into both), while a row filed
// by a process with no run identity records nothing in `session_id`. Seeding
// BOTH columns with the creator — the first version of this helper did — hid a
// real defect in the reclaim (it looked up `session_id` instead of `task_id`
// and reported every genuine creator as "unknown"); it passed here and failed
// on the first real tick. The fixture must reproduce the shape that is really
// on the ledger, not the shape that is convenient.
func seedCreatorRun(t *testing.T, creator, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, created_at, updated_at, tags)
		 VALUES (?, ?, ?, '', 1, 1, '')`,
		creator, "the run that filed the fixtures", status); err != nil {
		t.Fatalf("seed creator run: %v", err)
	}
}

// TestCreatorRunIsFoundByTaskIDNotSessionID is the regression guard for the
// defect the FIRST REAL TICK found (and no unit test did): a creator's run row
// is identified by task_id, because a run's session id IS its task id. A
// creator whose row carries an EMPTY session_id must still be judged.
func TestCreatorRunIsFoundByTaskIDNotSessionID(t *testing.T) {
	fixtureReclaimLedger(t)
	seedCreatorRun(t, "creator-shape", taskStatusCancelled)

	status, found, err := taskSessionRunStatus("creator-shape")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !found {
		t.Fatalf("a creator's run row must be found by task_id (a run's session id is its task id), even with session_id empty")
	}
	if status != taskStatusCancelled {
		t.Fatalf("status: want %s, got %s", taskStatusCancelled, status)
	}
}

// TestTaskSubmitStampsTheCreatorSession is ① — the row's creator is READABLE by
// a machine, from both the ledger and the session's metadata.json.
func TestTaskSubmitStampsTheCreatorSession(t *testing.T) {
	fixtureReclaimLedger(t)
	t.Setenv(TaskExecutorSessionEnv, "creator-session-abc")

	// No explicit SessionID: the stamp is the RUN's own identity, which is the
	// case that used to record "".
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "fixture filed by a run",
		Tags:    tasktags.FixtureTag,
		Source:  "agent",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	id := taskIDFromSubmitOutput(t, out)

	row := taskRowByID(t, id)
	if got := taskSubmitterSessionOf(row); got != "creator-session-abc" {
		t.Fatalf("the row's creator must be the submitting run: want %q, got %q", "creator-session-abc", got)
	}
	// And `task show` renders it instead of the empty placeholder.
	show, err := TaskShow(id)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	if !strings.Contains(show, "creator-session-abc") {
		t.Fatalf("task show must name the creator session, got:\n%s", show)
	}
	if strings.Contains(show, "LLM session — (task_submit)") {
		t.Fatalf("task show still renders the empty-creator placeholder:\n%s", show)
	}
	// The session's metadata.json carries it under the NAMED key.
	root, err := config.GopiDir()
	if err != nil {
		t.Fatalf("profile dir: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, config.SessionsDirName, id, taskSessionMetadataFileName))
	if err != nil {
		t.Fatalf("read the session metadata.json: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("metadata.json is not JSON: %v", err)
	}
	if got, _ := meta[TaskSubmitCreatorSessionKey].(string); got != "creator-session-abc" {
		t.Fatalf("metadata.json[%s] must name the creator: want %q, got %q (file:\n%s)",
			TaskSubmitCreatorSessionKey, "creator-session-abc", got, string(b))
	}
}

// TestFixtureReclaimCancelsAFinishedCreatorsRow is ② — the arm FIRES.
func TestFixtureReclaimCancelsAFinishedCreatorsRow(t *testing.T) {
	fixtureReclaimLedger(t)
	id := submitFixtureAs(t, "creator-done", "decoration of a finished run")
	seedCreatorRun(t, "creator-done", taskStatusCompleted)

	res, err := FixtureReclaimSweep("")
	if err != nil {
		t.Fatalf("reclaim sweep: %v", err)
	}
	if len(res.Cancelled) != 1 || res.Cancelled[0].TaskID != id {
		t.Fatalf("the arm must cancel the fixture row %s, got cancelled=%+v kept=%+v", id, res.Cancelled, res.Kept)
	}
	row := taskRowByID(t, id)
	if row.Status != taskStatusCancelled {
		t.Fatalf("row status must be %s, got %s", taskStatusCancelled, row.Status)
	}
	if !strings.Contains(row.Reason, "creator-done") {
		t.Fatalf("the reason must NAME the creator, got %q", row.Reason)
	}
}

// TestFixtureReclaimKeepsALiveCreatorsRow is ③ — the direction that must never
// regress: a probe in active use is left alone.
func TestFixtureReclaimKeepsALiveCreatorsRow(t *testing.T) {
	fixtureReclaimLedger(t)
	id := submitFixtureAs(t, "creator-live", "decoration of a LIVE run")
	seedCreatorRun(t, "creator-live", taskStatusInProgress)

	res, err := FixtureReclaimSweep("")
	if err != nil {
		t.Fatalf("reclaim sweep: %v", err)
	}
	if len(res.Cancelled) != 0 {
		t.Fatalf("a live creator's fixture row must never be cancelled, got %+v", res.Cancelled)
	}
	if len(res.Kept) != 1 || res.Kept[0].TaskID != id {
		t.Fatalf("the row must be KEPT and reported, got kept=%+v", res.Kept)
	}
	if !strings.Contains(res.Kept[0].Reason, "creator-live") {
		t.Fatalf("a kept row must say WHY, got %q", res.Kept[0].Reason)
	}
	if row := taskRowByID(t, id); row.Status != taskStatusPending {
		t.Fatalf("the row must stay pending, got %s", row.Status)
	}
}

// TestFixtureReclaimKeepsAnUnknownCreatorsRow is ④ — Rule 15's direction.
func TestFixtureReclaimKeepsAnUnknownCreatorsRow(t *testing.T) {
	fixtureReclaimLedger(t)
	id := submitFixtureAs(t, "creator-nowhere", "decoration of a run this host never saw")

	res, err := FixtureReclaimSweep("")
	if err != nil {
		t.Fatalf("reclaim sweep: %v", err)
	}
	if len(res.Cancelled) != 0 {
		t.Fatalf("an unmeasurable creator must never be reaped, got %+v", res.Cancelled)
	}
	if len(res.Kept) != 1 {
		t.Fatalf("the row must be KEPT, got kept=%+v", res.Kept)
	}
	if !strings.Contains(res.Kept[0].Reason, "unknown") {
		t.Fatalf("the kept reason must state that the state is UNKNOWN, got %q", res.Kept[0].Reason)
	}
	if row := taskRowByID(t, id); row.Status != taskStatusPending {
		t.Fatalf("the row must stay pending, got %s", row.Status)
	}
}

// TestFixtureReclaimIgnoresRowsWithoutTheCreatorAndNonFixtures pins the two
// rows the arm must not even consider: a fixture row with NO creator (kept,
// unknown) and a NON-fixture pending row whose creator is long gone (untouched
// — the arm's subject is the reserved tag, never ordinary work).
func TestFixtureReclaimLeavesOrdinaryWorkAlone(t *testing.T) {
	fixtureReclaimLedger(t)
	// An ordinary pending row from the same finished creator.
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:   "real work, do not reap",
		Payload:   "the briefing that makes the row dispatchable",
		Source:    "agent",
		SessionID: "creator-done",
	})
	if err != nil {
		t.Fatalf("submit normal row: %v", err)
	}
	normalID := taskIDFromSubmitOutput(t, out)
	seedCreatorRun(t, "creator-done", taskStatusVerified)

	if _, err := FixtureReclaimSweep(""); err != nil {
		t.Fatalf("reclaim sweep: %v", err)
	}
	if row := taskRowByID(t, normalID); row.Status != taskStatusPending {
		t.Fatalf("a NON-fixture pending row must never be touched, got %s", row.Status)
	}
}
