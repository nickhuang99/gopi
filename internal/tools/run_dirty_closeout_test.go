package tools

// The close-out check's SECOND shape (epic "a run must leave its worktree
// clean", 2026-09-16): a worktree left dirty WITH TRACKED CONTENT.
//
// The untracked half already existed (run_untracked_residue_test.go). This
// file covers the shape the epic calls the WORSE one: real work that was never
// committed. The branch stays invisible to every merge path (an unremovable
// dirty worktree is never picked up) while the task row says `completed` — so
// the run's own trail must say it, in the classifier's own words, with the
// offending entries quoted back.
//
// Nothing here weakens the "uncommitted work is work" guards (the epic's
// explicit prohibition): the tracked shape is RECORDED and is never a reason to
// touch, clean or recycle a tree. The sweep's reclaimSkipDirtyReason refusal is
// untouched.
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReclaimRunResidueTrackedWorkIsRecordedAtCloseout: a run that closes out
// `completed` with a MODIFIED TRACKED file in its own worktree gets an
// attributed record on its own trail, naming the shape and the entry.
func TestReclaimRunResidueTrackedWorkIsRecordedAtCloseout(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-trk", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-trk", repo, branch, wt)
	if _, err := TaskStart("t-trk", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// work.txt is COMMITTED on the fixture branch — editing it is an
	// uncommitted deliverable, not scratch. The writeback is REFUSED on this
	// tree (the binding gate); that refusal is what must not swallow the
	// RECORD this file pins.
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-trk", TaskStatusCompleted, "done"); err == nil {
		t.Fatalf("the tracked dirty shape must be REFUSED the %s claim (binding gate)", TaskStatusCompleted)
	}
	detail := taskEventDetailFor(t, "t-trk", reclaimRunResidueDirtyEvent)
	if detail == "" {
		t.Fatalf("UNCOMMITTED DELIVERABLE: the run closed out %s with a tracked change still "+
			"uncommitted in its own worktree (%s) and NOTHING on the trail says so — the branch is "+
			"then invisible to the merge queue while the row reads completed", TaskStatusCompleted, wt)
	}
	for _, want := range []string{reclaimSkipDirtyReason, "t-trk", wt, "work.txt"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the close-out record must name %q — got %q", want, detail)
		}
	}
}

// taskEventDetailFor returns the most recent detail filed under event for a
// row ("" when there is none) — the read side of "is the violation visible?".
func taskEventDetailFor(t *testing.T, taskID, event string) string {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var detail string
	err := db.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT 1`,
		taskID, event).Scan(&detail)
	if err != nil {
		return ""
	}
	return detail
}

// TestReclaimRunResidueNoteCoversBothShapes pins the facade: ONE entry point
// answers for every dirty close-out, and the two shapes are told apart by the
// classifier's own wording — the untracked sentence for litter, the tracked
// sentence for an uncommitted deliverable, nothing at all for a clean tree.
func TestReclaimRunResidueNoteCoversBothShapes(t *testing.T) {
	for _, tc := range []struct{ name, porcelain, want, notWant string }{
		{"clean", "", "", reclaimUntrackedResidueMarker},
		{"untracked only", "?? scratch.txt", reclaimUntrackedResidueMarker, reclaimSkipDirtyReason},
		{"tracked modified", " M a.txt", reclaimSkipDirtyReason, reclaimUntrackedResidueMarker},
		{"tracked plus untracked", "?? remnant.tmp\n M a.txt", reclaimSkipDirtyReason, reclaimUntrackedResidueMarker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reclaimRunResidueNote("t-x", "/wt", tc.porcelain)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("a clean tree must produce no note, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("the note must carry the classifier's own wording for this shape (%q), got %q", tc.want, got)
			}
			// The MIXED case belongs to the tracked sentence: the work is the
			// thing at risk, and the untracked half of the same list is visible
			// in the quoted entries anyway.
			if strings.Contains(got, tc.notWant) {
				t.Errorf("the other shape's wording must not appear (%q) — got %q", tc.notWant, got)
			}
			if !strings.Contains(got, "/wt") {
				t.Errorf("the note must name the worktree, got %q", got)
			}
		})
	}
}

// TestReclaimRunResidueEnforceSwitchNeverRefusesTrackedWork: the operator's
// hard-stop switch is scoped to the run's OWN UNTRACKED residue. The TRACKED
// shape has its own, DEFAULT-ON binding gate (the epic's measured case: a dirty
// close-out is not `completed`), and that gate declines a tree another row's
// live claim holds — but it is never the operator's untracked switch that acts
// on somebody's work, and this switch alone must not be able to soften the
// tracked gate. Asserted in both directions: switch OFF (the default) and switch
// ON both leave the tracked gate in charge.
func TestReclaimRunResidueEnforceSwitchNeverRefusesTrackedWork(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-trk-hard", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-trk-hard", repo, branch, wt)
	if _, err := TaskStart("t-trk-hard", "worker-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("uncommitted deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(reclaimEnvEnforce, reclaimEnvDirtyCloseoutOn)
	_, err := TaskRunWriteback("t-trk-hard", TaskStatusCompleted, "done")
	if err == nil {
		t.Fatalf("the untracked switch must not be what decides the TRACKED shape: the binding gate "+
			"still refuses (row %s)", taskRowStatus(t, "t-trk-hard"))
	}
	if strings.Contains(err.Error(), reclaimUntrackedResidueMarker) {
		t.Errorf("the refusal must speak for the TRACKED shape in its own words, not the untracked "+
			"marker: %v", err)
	}
	// The shape is RECORDED regardless of which gate refused.
	if taskEventDetailFor(t, "t-trk-hard", reclaimRunResidueDirtyEvent) == "" {
		t.Fatal("the shape must still be RECORDED (the trail is independent of the refusal)")
	}
}

// TestReclaimRunResidueTrackedShapeHasItsOwnEvent keeps the two shapes
// grep-separable: the tracked event must not be the untracked one, and both
// must grow from the shared stem the idempotence guard probes.
func TestReclaimRunResidueTrackedShapeHasItsOwnEvent(t *testing.T) {
	if reclaimRunResidueDirtyEvent == reclaimRunResidueEvent {
		t.Fatal("the tracked shape needs its own event: an operator greps for the remedy they need")
	}
	for _, ev := range []string{reclaimRunResidueEvent, reclaimRunResidueDirtyEvent, reclaimRunResidueBlockedEvent} {
		if !strings.HasPrefix(ev, reclaimRunResidueUncleanEvent) {
			t.Errorf("event %q must grow from the shared stem %q (the idempotence probe reads the family)",
				ev, reclaimRunResidueUncleanEvent)
		}
	}
	if got := reclaimRunResidueKind(" M a.txt"); got != reclaimRunResidueDirtyEvent {
		t.Errorf("a tracked dirt must pick the tracked event, got %q", got)
	}
	if got := reclaimRunResidueKind("?? scratch.txt"); got != reclaimRunResidueEvent {
		t.Errorf("an untracked dirt must pick the untracked event, got %q", got)
	}
}
