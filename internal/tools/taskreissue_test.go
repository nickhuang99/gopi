package tools

// Regression gates for failed-needs-reissue visibility (internal/tools/
// taskreissue.go).
//
// THE DEFECT these tests exist for, in one sentence: a `failed` row is
// TERMINAL by design and a reissue is a MANUAL act, so a failure with no
// successor row and no `resolved` tag left the system without ever being
// mentioned again — measured on the operator's ledger at 288 of 289 failed
// rows, none of them counted anywhere.
//
// The two arms below are the predicate's NEGATIVE half, which is the half a
// naive `WHERE status='failed'` implementation gets wrong: a row that WAS
// reissued and a row the operator has ADJUDICATED must both drop out of the
// count, or the note becomes a permanent, ignorable "N" that an operator
// learns to skip — the exact failure mode the mechanism is meant to end.
//
// Database: testutil.NewTestDBPath + the tools layer (Rule 32) — the fixture
// is a real seeded ledger opened through the same API the CLI uses, never a
// hand-rolled path, and it never touches the operator's profile.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// seedReissueRow writes one row through the ledger's OWN API — TaskNewOID
// creates the schema on first use and stamps the row in_progress (the state
// every task actually starts in), then TaskUpdate performs the transition
// under test. Nothing here reaches into SQL for the row's shape: the fixture
// is built from the same verbs an operator's ledger grew from.
//
// parent/tags are set where the row is BORN (TaskNewOID carries both), while
// status is the ONE thing a transition may change — which is why the row is
// never inserted already-failed.
func seedReissueRow(t *testing.T, id, status, tags, parent, deliverable string) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{
		TaskID:       id,
		Summary:      "fixture row " + id,
		Tags:         tags,
		ParentTaskID: parent,
		Deliverable:  deliverable,
		// A BRIEFING, so the row is an ACTIONABLE failure — a row somebody
		// asked something of. It is not decoration: since the bucket split
		// (epic failed-reissue-bucket), a failed row with no payload AND no
		// ground is a bare-prompt/chat registration that owes nothing, so a
		// fixture without one would put every assertion below in the NOT-debt
		// bucket and measure the opposite population.
		Payload: "the briefing this fixture row was asked to execute",
	}); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	if status == taskStatusInProgress {
		return // the create path's own state — no transition to make
	}
	if _, err := TaskUpdate(id, status); err != nil {
		t.Fatalf("update %s to %s: %v", id, status, err)
	}
}

// seedReissueSuccessor writes the PENDING reissue child of a failed parent —
// the row a successor mint actually produces (TaskCloneWithOptions / `task submit
// --parent-task-id`). It is built by the submit funnel's own creator, because
// `pending` is a state a row is BORN in, never a transition from in_progress.
func seedReissueSuccessor(t *testing.T, id, parent string) {
	t.Helper()
	ctx := context.Background()
	db := openReissueLedger(t)
	defer db.Close()
	if _, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID:       id,
		Summary:      "reissue of " + parent,
		Status:       taskStatusPending,
		ParentTaskID: parent,
		Payload:      "the rework briefing",
	}); err != nil {
		t.Fatalf("create successor %s: %v", id, err)
	}
}

// openReissueLedger opens the pinned test ledger for a read — through the same
// opener every sibling test uses (Rule 32: the scaffold owns the path).
func openReissueLedger(t *testing.T) *sql.DB {
	t.Helper()
	return openTestTasksDB(t, memoryDBPath())
}

// TestFailedNeedsReissueNamesOnlyOrphanFailures is the POSITIVE arm plus the
// two NEGATIVE arms the epic names:
//
//	orphan failed row                    → REPORTED (counted, id rendered)
//	failed row WITH a successor          → NOT reported (already reissued)
//	failed row tagged `resolved`         → NOT reported (already adjudicated)
//	failed row whose successor was made  → drops out when the child appears,
//	                                       so the count is a LIVE debt, not a
//	                                       historical tally
func TestFailedNeedsReissueNamesOnlyOrphanFailures(t *testing.T) {
	testutil.NewTestDBPath(t)
	ctx := context.Background()

	seedReissueRow(t, "t-orphan-fail", taskStatusFailed, "", "", "")
	seedReissueRow(t, "t-reissued-fail", taskStatusFailed, "", "", "")
	// The successor: a pending reissue child pointing at its failed parent —
	// exactly what gopi task submit --parent-task-id (or gopi task clone)
	// mints.
	seedReissueSuccessor(t, "t-reissued-fail-r1", "t-reissued-fail")
	seedReissueRow(t, "t-adjudicated-fail", taskStatusFailed, taskTagResolved, "", "")

	db := openReissueLedger(t)
	defer db.Close()

	rows, err := FailedNeedsReissue(ctx, db)
	if err != nil {
		t.Fatalf("FailedNeedsReissue: %v", err)
	}
	if len(rows) != 1 {
		var got []string
		for _, r := range rows {
			got = append(got, r.TaskID)
		}
		t.Fatalf("FailedNeedsReissue returned %d row(s) %v, want exactly 1:\n"+
			"  t-orphan-fail       must be reported (failed, no successor, no %q tag)\n"+
			"  t-reissued-fail     must NOT be reported (its successor t-reissued-fail-r1 exists)\n"+
			"  t-adjudicated-fail  must NOT be reported (tagged %q)",
			len(rows), got, taskTagResolved, taskTagResolved)
	}
	if rows[0].TaskID != "t-orphan-fail" {
		t.Fatalf("reported row is %q, want t-orphan-fail", rows[0].TaskID)
	}

	note := FailedReissueNote(rows)
	if !strings.Contains(note, TaskFailedReissuePrefix) {
		t.Fatalf("note does not carry the count-line prefix %q:\n%s", TaskFailedReissuePrefix, note)
	}
	if !strings.Contains(note, "t-orphan-fail") {
		t.Fatalf("note does not NAME the id an operator has to open:\n%s", note)
	}
	if strings.Contains(note, "t-reissued-fail") || strings.Contains(note, "t-adjudicated-fail") {
		t.Fatalf("note names a row that is not a debt:\n%s", note)
	}
	// The note ENDS with the two verbs that act on the ids it just named —
	// copy-pasteable, and on the SAME line (one log line, one webhook body).
	body := strings.TrimSuffix(note, "\n")
	if strings.Contains(body, "\n") {
		t.Fatalf("the note must stay ONE line (notify summary reuses it):\n%q", note)
	}
	if !strings.HasSuffix(body, strings.TrimSpace(taskFailedReissueNextFmt)) {
		t.Fatalf("the note does not end with the actionable verbs %q:\n%s", taskFailedReissueNextFmt, note)
	}
	for _, want := range []string{"gopi session resume <id>", "gopi task clone <id>", "gopi task archive <id>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the note does not carry the executable command %q:\n%s", want, note)
		}
	}

	// The zero contract: once the orphan is itself reissued, the mechanism
	// renders NOTHING — a zero line is noise, and noise is what stops an
	// alarm from being read.
	seedReissueSuccessor(t, "t-orphan-fail-r1", "t-orphan-fail")
	rows, err = FailedNeedsReissue(ctx, db)
	if err != nil {
		t.Fatalf("FailedNeedsReissue after reissue: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a reissued failure is still counted (%d rows) — the note would never retire", len(rows))
	}
	if note := FailedReissueNote(rows); note != "" {
		t.Fatalf("FailedReissueNote rendered %q for zero rows, want \"\"", note)
	}
}

// TestFailedReissueNoteIsOnTheListRender is the surface arm: `gopi task list`
// (tools.TaskList) carries the line, and a ledger whose failures are all
// handled carries none — so the empty ledger's "No tasks found." is never
// contradicted by a stale note.
func TestFailedReissueNoteIsOnTheListRender(t *testing.T) {
	testutil.NewTestDBPath(t)

	rendered, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList on an empty ledger: %v", err)
	}
	if strings.Contains(rendered, TaskFailedReissuePrefix) {
		t.Fatalf("an empty ledger rendered the reissue note:\n%s", rendered)
	}

	seedReissueRow(t, "t-listed-fail", taskStatusFailed, "", "", "")
	rendered, err = TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(rendered, TaskFailedReissuePrefix+"1 ") {
		t.Fatalf("TaskList does not carry the reissue count line:\n%s", rendered)
	}
	if !strings.Contains(rendered, "t-listed-fail") {
		t.Fatalf("TaskList's reissue line does not name the row:\n%s", rendered)
	}
}
