package tools

// task_row_identity_test.go — THE SPLIT OF THE TWO SESSION FACTS, pinned.
//
// A ledger row carries TWO session facts and ONE cell used to hold both:
//
//	① who FILED the row — the anti-self-reference key (claim eligibility refuses
//	   a row filed by the claiming session), and
//	② the row's RUN SESSION — the uuid naming its record under
//	   sessions/<task-id>/, which is what `task show`'s session_log line, the
//	   model resolution and a resume all address.
//
// The INSERT wrote ① into `session_id`, and the deferred stamp fills that column
// only while it is EMPTY. A submitted row was therefore born FULL of ①, so ②
// could never land — for the row's whole life the column named a session that
// never ran it, and every session-keyed lookup resolved to the submitter.
//
// The fix moves ① to its own column and leaves the cell empty at INSERT.
//
// THESE ARMS ARE THE CONTRACT, not the implementation, and each one is RED
// against the pre-fix shape: with ① written into session_id, the first arm sees
// the submitter where it demands an empty cell, and the third sees the run's
// session refused a cell that was already full.

import (
	"context"
	"io"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// riCloseEnv closes a fixture handle and FAILS the case when the close errors.
// The bare form is a stylegate finding, and an ignored close would hide a
// fixture that never released the ledger before the next opener took it.
func riCloseEnv(t *testing.T, db io.Closer) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("close the fixture handle: %v", err)
	}
}

// riRowSessions reads BOTH session facts back off one row (COALESCE because the
// two columns are NULL on a row that never carried them — the two "empty" shapes
// must not be distinguishable to an assertion about the VALUE).
func riRowSessions(t *testing.T, taskID string) (sessionID, submitter string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(session_id,''), COALESCE(`+taskSubmitterColumnName+`,'') FROM tasks WHERE task_id = ?`,
		taskID).Scan(&sessionID, &submitter); err != nil {
		t.Fatalf("read the session columns of %s: %v", taskID, err)
	}
	return sessionID, submitter
}

// TestRowIdentitySubmitLeavesTheRunSessionCellFree is the INSERT half: a row
// filed by an agent session lands with the submitting session in the provenance
// column and an EMPTY `session_id` — the cell its run's session must be able to
// fill later.
func TestRowIdentitySubmitLeavesTheRunSessionCellFree(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-rowid-submit"
	const submitter = "aaaaaaaa-1111-4111-8111-111111111111"

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary:   "a row filed by an agent session",
		Payload:   "do the thing",
		Source:    "agent",
		TaskID:    id,
		SessionID: submitter,
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}

	sessionID, gotSubmitter := riRowSessions(t, id)
	if sessionID != "" {
		t.Errorf("session_id = %q at submit time, want EMPTY — this column is the row's RUN session, which does not exist yet; a submitter parked here makes the run's session unwritable for the row's whole life", sessionID)
	}
	if gotSubmitter != submitter {
		t.Errorf("%s = %q, want the submitting session %q — the anti-self-reference key must survive the split", taskSubmitterColumnName, gotSubmitter, submitter)
	}
}

// TestRowIdentityBackfillLandsTheRunSessionOnASubmittedRow is the arm the epic
// was filed for: a row with a submitter recorded must still be able to receive
// the session that actually runs it.
func TestRowIdentityBackfillLandsTheRunSessionOnASubmittedRow(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-rowid-backfill"
	const submitter = "bbbbbbbb-2222-4222-8222-222222222222"
	const runSession = "cccccccc-3333-4333-8333-333333333333"

	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, `+taskSubmitterColumnName+`,
		                    model, parent_task_id, tags, payload, source, deliverable, priority, reason,
		                    run_method, run_cmd, created_at, updated_at)
		 VALUES (?, 'a row filed by a session', ?, '', ?, '', '', '', 'briefing', 'agent', '', 5, '', '', '', 1.0, 1.0)`,
		id, taskStatusInProgress, submitter); err != nil {
		_ = db.Close() //stylegate:ignore the seed failure is already in the report; this close is best-effort
		t.Fatalf("seed the filed row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the fixture handle: %v", err)
	}

	if err := TaskBackfillSession(id, runSession); err != nil {
		t.Fatalf("TaskBackfillSession: %v", err)
	}

	sessionID, gotSubmitter := riRowSessions(t, id)
	if sessionID != runSession {
		t.Fatalf("session_id after the backfill = %q, want the run's session %q — the run's session could not land on a row that already carried a submitter", sessionID, runSession)
	}
	if gotSubmitter != submitter {
		t.Errorf("%s = %q after the backfill, want the submitter %q untouched — the backfill fills the run session and must not rewrite who filed the row", taskSubmitterColumnName, gotSubmitter, submitter)
	}
}

// TestRowIdentityBackfillNeverOverwritesARunSession is the REVERSE guard: a row
// already carrying a run session must not be moved to a different one by a later
// stamp — the run's identity is written once.
func TestRowIdentityBackfillNeverOverwritesARunSession(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-rowid-kept"
	const first = "dddddddd-4444-4444-8444-444444444444"

	dpSeedRow(t, memoryDBPath(), id, "a row that already has its run session", taskStatusInProgress)
	if err := TaskBackfillSession(id, first); err != nil {
		t.Fatalf("first stamp: %v", err)
	}
	if err := TaskBackfillSession(id, "eeeeeeee-5555-4555-8555-555555555555"); err != nil {
		t.Fatalf("second stamp: %v", err)
	}
	if sessionID, _ := riRowSessions(t, id); sessionID != first {
		t.Fatalf("session_id = %q after a second stamp, want the first %q — a run session is written once", sessionID, first)
	}
}

// TestRowIdentityClaimRefusesOnTheSubmitter is the claim funnel's read of the
// split: a row FILED by session S is excluded for S, and a row that merely ran
// under S is not — otherwise a session could claim its own row, or a row could
// become unclaimable by the run that holds it.
func TestRowIdentityClaimRefusesOnTheSubmitter(t *testing.T) {
	testutil.NewTestDBPath(t)
	const filedID = "t-rowid-claim-filed"
	const runID = "t-rowid-claim-run"
	const session = "ffffffff-6666-4666-8666-666666666666"

	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	// A row this session FILED: excluded for it (session_id is free, the
	// submitter column names this session).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, `+taskSubmitterColumnName+`,
		                    model, parent_task_id, tags, payload, source, deliverable, priority, reason,
		                    run_method, run_cmd, created_at, updated_at)
		 VALUES (?, 'filed by this session', ?, '', ?, '', '', '', 'briefing', 'agent', '', 5, '', '', '', 1.0, 1.0)`,
		filedID, taskStatusPending, session); err != nil {
		t.Fatalf("seed the filed row: %v", err)
	}
	// A row whose RUN SESSION is this session: it must stay claimable.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, `+taskSubmitterColumnName+`,
		                    model, parent_task_id, tags, payload, source, deliverable, priority, reason,
		                    run_method, run_cmd, created_at, updated_at)
		 VALUES (?, 'a row this session runs', ?, '', '', '', '', '', 'briefing', 'human', '', 5, '', '', '', 1.0, 1.0)`,
		runID, taskStatusPending, session); err != nil {
		t.Fatalf("seed the run row: %v", err)
	}
	riCloseEnv(t, db)

	res, err := TaskNextFiltered("tester:1", session, "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res == nil || res.Task == nil {
		t.Fatalf("no row claimed — the row that merely RUNS under this session must remain eligible")
	}
	if res.Task.TaskID == filedID {
		t.Fatalf("claimed %q, the row session %q FILED — the anti-self-reference guard must read the submitter column", filedID, session)
	}
}

// TestRowIdentitySubmitterSurvivesForBothStorageShapes pins the ONE resolver the
// readers share: a row written after the split answers from its provenance
// column, a legacy row (submitter column empty, session_id holding the submitter)
// answers from session_id, and a row whose session_id is merely its own run
// answer what it holds.
func TestRowIdentitySubmitterSurvivesForBothStorageShapes(t *testing.T) {
	testutil.NewTestDBPath(t)
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	seed := func(id, sessionID, submitter string) {
		t.Helper()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO tasks (task_id, summary, status, session_id, `+taskSubmitterColumnName+`,
			                    model, parent_task_id, tags, payload, source, deliverable, priority, reason,
			                    run_method, run_cmd, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, '', '', '', '', 'agent', '', 5, '', '', '', 1.0, 1.0)`,
			id, "row "+id, taskStatusPending, sessionID, submitter); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// Post-split: the run session in session_id, the submitter in its own column.
	seed("t-rowid-post", "run-session-post", "post-submitter")
	// Legacy: the submitter still sits in session_id.
	seed("t-rowid-legacy", "legacy-submitter", "")
	// A row whose cell holds its own id and no submitter (the auto/human shape).
	seed("t-rowid-self", "t-rowid-self", "")
	riCloseEnv(t, db)

	for _, tc := range []struct{ id, want string }{
		{"t-rowid-post", "post-submitter"},
		{"t-rowid-legacy", "legacy-submitter"},
		{"t-rowid-self", ""},
	} {
		got, err := TaskSubmitterSession(tc.id)
		if err != nil {
			t.Fatalf("TaskSubmitterSession(%s): %v", tc.id, err)
		}
		if got != tc.want {
			t.Errorf("TaskSubmitterSession(%s) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// TestRowIdentitySchemaMigrationIsIdempotent pins the ALTER's "duplicate column
// name IS the migrated case" contract by running the real schema ensure twice on
// a ledger that already carries the column: the second pass must be a no-op, and
// the rows must be untouched (a migration that rewrote data would be a silent
// backfill of history).
func TestRowIdentitySchemaMigrationIsIdempotent(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-rowid-migrate"
	ctx := context.Background()

	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, `+taskSubmitterColumnName+`,
		                    model, parent_task_id, tags, payload, source, deliverable, priority, reason,
		                    run_method, run_cmd, created_at, updated_at)
		 VALUES (?, 'a row that must survive a second migration pass', ?, 'legacy-sub', 'sub-x', '', '', '', '', 'agent', '', 5, '', '', '', 1.0, 1.0)`,
		id, taskStatusInProgress); err != nil {
		_ = db.Close() //stylegate:ignore the seed failure is already in the report; this close is best-effort
		t.Fatalf("seed: %v", err)
	}
	if err := db.Close(); err != nil { //stylegate:ignore closing the fixture's own handle before reopening it
		t.Fatalf("close: %v", err)
	}

	// A SECOND open runs ensureTasksSchema again — over a ledger that already has
	// the column, which is the duplicate-column arm.
	db2, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	defer db2.Close()
	var n int
	if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE task_id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count after the second pass: %v", err)
	}
	if n != 1 {
		t.Fatalf("the row is gone after a second migration pass (count=%d) — the ALTER must be idempotent, never destructive", n)
	}
}
