package tools

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// task_resume_new_session_test.go — the TRAIL half of epic 01a17df9-99c4.
//
// The stderr warning is the operator's copy; the ROW is everybody else's. These
// arms pin three facts that the stderr half cannot:
//
//  1. a resume that started a NEW session writes `resume-new-session` on the
//     row, so `resumed:<session>` is not the only trace of a run that did not
//     continue the session it named;
//  2. an in-place REBUILD writes `resume-record-rebuilt` — milder than a mint,
//     but still a fact about who wrote the record;
//  3. `task show` RENDERS both, because an event in task_events that the show
//     skips is a record only a hand-written SQL query can find (the measured
//     defect the verdictTrailEventNames list exists for).
//
// The empty-taskID arm is the fourth: a session-only resume (no --task, no row
// resolved from the target) must not fail on a trail it has nothing to write to.

const resumeNewSessionTestID = "01a17df9-99c4-trail"

// resumeTrailFixture seeds one row in a fresh ledger and returns the DB path.
func resumeTrailFixture(t *testing.T, id string) string {
	t.Helper()
	// The scaffold PINS the ledger env itself (Rule 32) — no hand-rolled path,
	// no second spelling of the variable.
	dbPath := testutil.NewTestDBPath(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		TaskID: id, Summary: "resume trail", Payload: "payload", Source: "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
	return dbPath
}

// TestTaskResumeNewSessionNoteRecordsOnTheRow is the writer half.
func TestTaskResumeNewSessionNoteRecordsOnTheRow(t *testing.T) {
	const id = resumeNewSessionTestID + "-new"
	dbPath := resumeTrailFixture(t, id)

	detail := TaskResumeNewSessionNoteText("01a1ffff-0000", "/sessions/01a1ffff-0000", "GOPI_RESUME_ALLOW_NEW_SESSION")
	if err := TaskResumeNewSessionNote(id, detail); err != nil {
		t.Fatalf("note: %v", err)
	}
	if n := dpEventCount(t, dbPath, id, ResumeNewSessionEvent); n != 1 {
		t.Fatalf("the row carries %d %s record(s), want 1 — a resume that started a NEW session left no trace on its row",
			n, ResumeNewSessionEvent)
	}
	// A second identical note is the SAME fact retried (a wrapper reruns the
	// same resume); it must not drip a second copy onto the trail.
	if err := TaskResumeNewSessionNote(id, detail); err != nil {
		t.Fatalf("second note: %v", err)
	}
	if n := dpEventCount(t, dbPath, id, ResumeNewSessionEvent); n != 1 {
		t.Errorf("a repeated note appended %d records, want 1 (the dedup bound is %d)", n, resumeNewSessionRecordLimit)
	}
}

// TestTaskResumeRecordRebuiltNoteRecordsOnTheRow is the recovery's half.
func TestTaskResumeRecordRebuiltNoteRecordsOnTheRow(t *testing.T) {
	const id = resumeNewSessionTestID + "-rebuilt"
	dbPath := resumeTrailFixture(t, id)

	dir := "/sessions/01a17df9-99c4-rebuilt"
	if err := TaskResumeRecordRebuiltNote(id, TaskResumeRecordRebuiltNoteText("01a17df9-99c4-rebuilt", dir)); err != nil {
		t.Fatalf("note: %v", err)
	}
	if n := dpEventCount(t, dbPath, id, ResumeRecordRebuiltEvent); n != 1 {
		t.Fatalf("the row carries %d %s record(s), want 1", n, ResumeRecordRebuiltEvent)
	}
}

// TestTaskResumeNotesAreNoOpsWithoutARow: an empty task id is the normal case
// for an operator's bare `gopi chat --resume`, and it must not be an error. The
// stderr line the caller prints is the only report such a run has, which is
// exactly right — there is no row to tell.
func TestTaskResumeNotesAreNoOpsWithoutARow(t *testing.T) {
	testutil.NewTestDBPath(t)
	if err := TaskResumeNewSessionNote("", "detail"); err != nil {
		t.Errorf("an empty task id must be a no-op, got: %v", err)
	}
	if err := TaskResumeRecordRebuiltNote("", "detail"); err != nil {
		t.Errorf("an empty task id must be a no-op, got: %v", err)
	}
}

// TestTaskResumeNotesRefuseAnEmptyDetail: an entry whose body says nothing is a
// record a later reader cannot act on, and the writer is the only place that can
// still refuse it (Rule 15 — no silent, contentless success).
func TestTaskResumeNotesRefuseAnEmptyDetail(t *testing.T) {
	const id = resumeNewSessionTestID + "-emptydetail"
	resumeTrailFixture(t, id)
	if err := TaskResumeNewSessionNote(id, "  "); err == nil {
		t.Error("an empty detail was accepted — the trail would carry an event that names nothing")
	}
	if err := TaskResumeRecordRebuiltNote(id, ""); err == nil {
		t.Error("an empty detail was accepted — the trail would carry an event that names nothing")
	}
}

// TestTaskResumeNotesAreRenderedByTaskShow pins the VISIBILITY half: membership
// in verdictTrailEventNames is what makes `task show` print the line. Without it
// both events sit in task_events and a reviewer reading the row cannot tell a
// continued session from a minted one (measured shape: the same omission hid
// `delivery-proof-unverified` and the two writeback-refusal events).
func TestTaskResumeNotesAreRenderedByTaskShow(t *testing.T) {
	for _, event := range []string{ResumeNewSessionEvent, ResumeRecordRebuiltEvent} {
		if !taskShowVerdictEventName(event) {
			t.Errorf("%q is not a verdict-trail event, so `task show` would not render it at all", event)
		}
	}
	if got, want := len(verdictTrailEventNames), len(resumeTrailExpectedMembers()); got < want {
		t.Fatalf("verdictTrailEventNames carries %d names, want at least %d (the list only ratchets UP)", got, want)
	}

	const id = resumeNewSessionTestID + "-show"
	resumeTrailFixture(t, id)
	if err := TaskResumeNewSessionNote(id, TaskResumeNewSessionNoteText("01a1ffff-0000", "/sessions/01a1ffff-0000", "GOPI_RESUME_ALLOW_NEW_SESSION")); err != nil {
		t.Fatalf("note: %v", err)
	}
	out, err := TaskShow(id)
	if err != nil {
		t.Fatalf("task show %s: %v", id, err)
	}
	if !strings.Contains(out, ResumeNewSessionEvent) {
		t.Errorf("`task show` does not render %s — the row would read as a normal resume:\n%s", ResumeNewSessionEvent, out)
	}
}

// resumeTrailExpectedMembers is the floor for the list-length assertion above: a
// deletion would otherwise pass the per-event membership checks while dropping
// every OTHER name from the list.
func resumeTrailExpectedMembers() []string {
	return []string{ResumeNewSessionEvent, ResumeRecordRebuiltEvent, DeliveryProofUnverifiedEvent}
}

// TestTaskResumeNewSessionNoteTextNamesTheThreeFacts keeps the sentence honest:
// the target, the directory and the switch. A note that dropped the switch would
// leave a reader unable to reproduce the decision, and one that dropped the
// directory would name no session.
func TestTaskResumeNewSessionNoteTextNamesTheThreeFacts(t *testing.T) {
	got := TaskResumeNewSessionNoteText("01a1ffff-0000", "/sessions/01a1ffff-0000", "GOPI_RESUME_ALLOW_NEW_SESSION")
	for _, want := range []string{"01a1ffff-0000", "/sessions/01a1ffff-0000", "GOPI_RESUME_ALLOW_NEW_SESSION"} {
		if !strings.Contains(got, want) {
			t.Errorf("the trail sentence does not name %q: %s", want, got)
		}
	}
}
