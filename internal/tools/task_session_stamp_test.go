package tools

// The session_id PROVENANCE defect, red-to-green (epic 01a17df9).
//
// THE MEASURED DEFECT: two worker-claimed rows carried the SAME chat session id
// (07d98407-…, the console's own session) while sessions/<row>/ held the record
// of the run that really executed them. The write source could fall back to the
// newest *.jsonl under the profile's sessions ROOT — the legacy FLAT layout —
// and a chat session is written CONTINUOUSLY, so inside any run window its flat
// record is newer than the row's own. The value went into the column through a
// fill-only-while-empty guard, so nothing could ever correct it.
//
// These arms pin the two halves that replace that behaviour, on the production
// surfaces (tools.TaskBackfillSession, tools.TaskRepairSessionIDs) with a REAL
// ledger row (testutil.NewTestDBPath, Rule 32) and REAL run directories in the
// task-run layout — never a fabricated map.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// The ids used below are spelled ONCE (Rule 1) so a test that names the
	// wrong one fails loudly instead of comparing two literals.
	stampForeignChatID = "07d98407-41a5-42db-875c-de0b077c790a"
	stampOwnSessionID  = "8b9a0155-dd2a-4949-b902-97fbfe75aa84"
	stampOtherRowID    = "01a0d9ab-7f5a-75f4-9be4-f842fa1449cf"
	stampOtherRowSess  = "f87bfda6-1463-462c-a7a4-af46008f30b5"
)

// stampTestProfile pins GOPI_HOME at a per-test directory and gives the test its
// own ledger, so both the run directories read below and the row judged are this
// test's — never the operator's live profile (Rule 32).
func stampTestProfile(t *testing.T) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
	testutil.NewTestDBPath(t)
}

// stampTestWriteRecord writes ONE session record at the task-run layout
// sessions/<task-id>/<session-id>.jsonl (or, with taskID == "", at the LEGACY
// FLAT root address — the shape that polluted the two rows).
func stampTestWriteRecord(t *testing.T, taskID, sessionID string) {
	t.Helper()
	// gopiDataRoot is the PROFILE dir (GOPI_HOME + ".gopi") — the same root
	// config.GopiDir hands every production reader, so the fixture and the
	// code under test cannot disagree about where a record lives.
	root := filepath.Join(gopiDataRoot(), config.SessionsDirName)
	dir := root
	if taskID != "" {
		dir = filepath.Join(root, taskID)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	body := `{"type":"session_start","sessionId":"` + sessionID + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionID+taskSessionJSONLExt), []byte(body), 0o644); err != nil {
		t.Fatalf("write record %s: %v", sessionID, err)
	}
}

// stampTestSeedRow files one ledger row carrying a session_id COLUMN.
func stampTestSeedRow(t *testing.T, sessionID string) string {
	return stampTestSeedRowAs(t, sessionID, taskStatusInProgress)
}

// stampTestSeedRowAs files the row in an explicit STATUS — the repair pass
// rewrites only TERMINAL rows (a live row's column feeds the claim guard), so
// both sides of that line have to be constructible.
func stampTestSeedRowAs(t *testing.T, sessionID, status string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	id := idForTest("stp")
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks
		(task_id, summary, status, session_id, model, parent_task_id, tags, payload,
		 source, deliverable, priority, reason, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "a row the session-provenance tests judge", status, sessionID, "", "",
		"", "", tasksource.Agent, "", 0, "", 1.0, 1.0); err != nil {
		t.Fatalf("insert row %s: %v", id, err)
	}
	return id
}

// stampTestColumn reads the row's session_id back off the LEDGER — the only
// evidence a write happened (Rule 31: query what the write claims to change).
func stampTestColumn(t *testing.T, taskID string) string {
	t.Helper()
	got, err := TaskSessionID(taskID)
	if err != nil {
		t.Fatalf("read column of %s: %v", taskID, err)
	}
	return got
}

// TestTaskBackfillSessionAcceptsOnlyTheRowsOwnRecord is the RED-TO-GREEN arm for
// the WRITE SOURCE: a foreign session id — one whose record sits at the legacy
// flat root, which is where a continuously written chat session lives — is
// REFUSED by name and the column stays empty, while the row's own record is
// accepted.
func TestTaskBackfillSessionAcceptsOnlyTheRowsOwnRecord(t *testing.T) {
	stampTestProfile(t)
	id := stampTestSeedRow(t, "")
	stampTestWriteRecord(t, "", stampForeignChatID) // the pollution source

	err := TaskBackfillSession(id, stampForeignChatID)
	if err == nil {
		t.Fatal("a session record outside the row's own run directory must be REFUSED, not stamped")
	}
	for _, want := range []string{stampForeignChatID, id} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must name %q:\n%v", want, err)
		}
	}
	if got := stampTestColumn(t, id); got != "" {
		t.Fatalf("column = %q after a refused stamp, want it left empty", got)
	}

	// The row's OWN record is the accepted source.
	stampTestWriteRecord(t, id, stampOwnSessionID)
	if err := TaskBackfillSession(id, stampOwnSessionID); err != nil {
		t.Fatalf("the row's own record must be accepted: %v", err)
	}
	if got := stampTestColumn(t, id); got != stampOwnSessionID {
		t.Fatalf("column = %q, want the row's own record %q", got, stampOwnSessionID)
	}

	// And a SECOND foreign id cannot replace it (the fill-only-while-empty guard
	// is unchanged — a wrong value is repaired by the pass below, not by luck).
	stampTestWriteRecord(t, stampOtherRowID, stampOtherRowSess)
	if err := TaskBackfillSession(id, stampOtherRowSess); err == nil {
		t.Fatal("another row's run record must be refused")
	}
	if got := stampTestColumn(t, id); got != stampOwnSessionID {
		t.Fatalf("column = %q, want it unmoved at %q", got, stampOwnSessionID)
	}
}

// TestTaskRepairSessionIDsRepairsOnlyAnExecutorStampFromAnotherRow is the arm
// for the REPAIR DOOR (deliverable ③), and it pins BOTH directions:
//
//   - an executor stamp that names ANOTHER ROW's run record is provably wrong
//     and is rewritten to the row's own record (dry run first: it must change
//     nothing, and report),
//   - a SUBMITTER-shaped value (a session that no row directory holds — here the
//     chat session that actually polluted the two rows) is LEFT ALONE, with its
//     reason printed: the column's submitter semantics are load bearing (the
//     anti-self-reference claim guard compares it, see task_resume_session.go),
//     so overwriting it would break the guard on every agent-filed row.
func TestTaskRepairSessionIDsRepairsOnlyAnExecutorStampFromAnotherRow(t *testing.T) {
	stampTestProfile(t)

	// ROW A: polluted by another row's executor stamp; its own record exists.
	polluted := stampTestSeedRowAs(t, stampOtherRowSess, TaskStatusFailed)
	stampTestWriteRecord(t, polluted, stampOwnSessionID)
	stampTestWriteRecord(t, stampOtherRowID, stampOtherRowSess)

	// ROW B: a submitter-shaped value (the chat session, flat at the root).
	submitter := stampTestSeedRowAs(t, stampForeignChatID, TaskStatusCompleted)
	stampTestWriteRecord(t, "", stampForeignChatID)

	dry, err := TaskRepairSessionIDs(polluted, false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun {
		t.Error("apply=false must be a DRY RUN")
	}
	if len(dry.Candidates) != 1 {
		t.Fatalf("dry run candidates = %d, want 1 (%+v)", len(dry.Candidates), dry)
	}
	if c := dry.Candidates[0]; c.Applied || c.Repaired != stampOwnSessionID {
		t.Errorf("dry run candidate = %+v, want WOULD REPAIR to %q and Applied=false", c, stampOwnSessionID)
	}
	if got := stampTestColumn(t, polluted); got != stampOtherRowSess {
		t.Fatalf("dry run wrote: column = %q, want %q untouched", got, stampOtherRowSess)
	}

	applied, err := TaskRepairSessionIDs(polluted, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(applied.Candidates) != 1 || !applied.Candidates[0].Applied {
		t.Fatalf("apply candidates = %+v, want one APPLIED", applied.Candidates)
	}
	if got := stampTestColumn(t, polluted); got != stampOwnSessionID {
		t.Fatalf("column = %q after repair, want the row's own record %q", got, stampOwnSessionID)
	}

	// The submitter-shaped row: dry AND apply both leave it, and both SAY so.
	for _, apply := range []bool{false, true} {
		rep, err := TaskRepairSessionIDs(submitter, apply)
		if err != nil {
			t.Fatalf("repair (apply=%v): %v", apply, err)
		}
		if len(rep.Candidates) != 0 {
			t.Errorf("apply=%v: a submitter session is not pollution — candidates = %+v", apply, rep.Candidates)
		}
		if len(rep.Skipped) != 1 {
			t.Fatalf("apply=%v: the row must be REPORTED as kept, got %+v", apply, rep.Skipped)
		}
		if got := stampTestColumn(t, submitter); got != stampForeignChatID {
			t.Errorf("apply=%v: the submitter column moved to %q — the claim guard depends on it", apply, got)
		}
	}
}

// TestTaskRepairSessionIDsSweepsEveryRowWhenNoIDIsGiven pins the no-operand
// shape: the pass exists for a POPULATION that predates any repair door, so
// naming no row must sweep them all rather than refuse.
func TestTaskRepairSessionIDsSweepsEveryRowWhenNoIDIsGiven(t *testing.T) {
	stampTestProfile(t)
	first := stampTestSeedRowAs(t, stampOtherRowSess, TaskStatusCompleted)
	second := stampTestSeedRowAs(t, stampOtherRowSess, TaskStatusCompleted)
	stampTestWriteRecord(t, first, stampOwnSessionID)
	stampTestWriteRecord(t, second, stampOwnSessionID)
	stampTestWriteRecord(t, stampOtherRowID, stampOtherRowSess)

	rep, err := TaskRepairSessionIDs("", false)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if rep.Scanned < 2 {
		t.Fatalf("scanned = %d, want both seeded rows", rep.Scanned)
	}
	if len(rep.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2 (%+v)", len(rep.Candidates), rep)
	}
}
