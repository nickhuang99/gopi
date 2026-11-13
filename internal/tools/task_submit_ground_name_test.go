package tools

// task_submit_ground_name_test.go — the SUBMIT MOMENT refuses a row id that can
// never name a ground, BEFORE anything is written (epic 01a12b5c, Rule 15).
//
// THE PROPERTY, in two halves:
//
//	① the PREDICATE fires exactly where the dispatch gate would fire — a
//	   non-uuid id on a repo-aware row — and nowhere else. The negative cases
//	   are the load-bearing ones: a fixture row with NO repo (the 96-call-site
//	   shape, memory #7578) and a full-uuid id must both pass untouched, or the
//	   check stops being an early refusal and becomes a blast radius.
//
//	② the ENGINE refuses, by name, and holds its "nothing was written"
//	   promise: the row does not exist afterwards and no session container
//	   appears. A refusal that still leaves a row is indistinguishable from the
//	   defect it replaces.

import (
	"strings"
	"testing"
)

// TestSubmitGroundNamePredicateFiresOnlyOnDerivingRows is ① — the truth table,
// including the two negatives that keep the check additive.
func TestSubmitGroundNamePredicateFiresOnlyOnDerivingRows(t *testing.T) {
	const (
		uuidID = "01a11500-0000-7000-8000-000000000041"
		slugID = "t-foo"
	)
	cases := []struct {
		name string
		id   string
		repo string
		want bool
	}{
		{"non-uuid id + repo: the row can never derive a ground", slugID, "/some/repo", true},
		{"non-uuid id, NO repo: the row is not repo-aware, nothing is derived", slugID, "", false},
		{"non-uuid id, whitespace repo: still not repo-aware", slugID, "   ", false},
		{"full uuid id + repo: a ground name IS derivable", uuidID, "/some/repo", false},
		{"full uuid id, NO repo: not repo-aware", uuidID, "", false},
		{"empty id: the auto-mint path mints a uuid, nothing to refuse", "", "/some/repo", false},
	}
	for _, c := range cases {
		if got := TaskSubmitGroundNameDenied(c.id, c.repo); got != c.want {
			t.Errorf("%s: TaskSubmitGroundNameDenied(%q, %q) = %v, want %v", c.name, c.id, c.repo, got, c.want)
		}
	}
	// The refusal wrapper is nil exactly when the predicate is false, so the
	// two cannot drift.
	if err := TaskSubmitGroundNameRefusal(uuidID, "/some/repo"); err != nil {
		t.Errorf("a full-uuid row must not be refused at submit time, got: %v", err)
	}
	if err := TaskSubmitGroundNameRefusal(slugID, ""); err != nil {
		t.Errorf("a row with no repo must not be refused at submit time, got: %v", err)
	}
	refusal := TaskSubmitGroundNameRefusal(slugID, "/some/repo")
	if refusal == nil {
		t.Fatal("a non-uuid id on a repo-aware row MUST be refused at submit time")
	}
	// The sentence is the dispatch gate's own (Rule 13/14: one definition, two
	// moments) — asserted by content, not by identity of the string, so a
	// reworded refusal still has to keep saying these four things.
	msg := refusal.Error()
	t.Logf("submit-time refusal for %q:\n%s", slugID, msg)
	for _, want := range []string{
		slugID,              // names the row
		"not a full uuid",   // names the cause
		"prefix/slug shape", // names the consequence
		TaskRepoRepairVerb,  // names the door
		TaskGroundDirPrefix, // names the shape a ground name would have
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the submit refusal must contain %q — an operator cannot act on it:\n%s", want, msg)
		}
	}
}

// TestSubmitRefusesANonUUIDRowIdBeforeWritingAnything is ② at the engine, driven
// through the REAL TaskSubmit funnel (the function both the CLI and the
// task_submit tool call).
func TestSubmitRefusesANonUUIDRowIdBeforeWritingAnything(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testGroundDBPath(t))
	repo := groundTestRepo(t, "submit-name-repo")

	const slugID = "t-foo"
	_, err := TaskSubmit(TaskSubmitOptions{
		Summary: "submit fixture " + slugID, Payload: "work", Repo: repo, TaskID: slugID,
		// THE OPERATOR FACE's flag: this is the posture `gopi task submit` puts
		// the engine in (cmd/agent/cli_tasks.go). A library caller that leaves
		// it false is untouched — that is the fixture boundary the budget run
		// measured.
		RefuseUngroundableID: true,
	})
	if err == nil {
		t.Fatalf("TaskSubmit(--task-id %s) was ACCEPTED — the row can never run "+
			"(no ground name can be derived from a non-uuid id) and dispatch will refuse it "+
			"after a worker has burned a claim", slugID)
	}
	msg := err.Error()
	t.Logf("engine refusal:\n%s", msg)
	for _, want := range []string{slugID, "not a full uuid", TaskRepoRepairVerb} {
		if !strings.Contains(msg, want) {
			t.Errorf("engine refusal does not contain %q:\n%s", want, msg)
		}
	}
	// WRITES NOTHING. Both halves are checked: no row, and no session container
	// (the container is the artifact an accepted submit creates even before the
	// row is visible).
	if TaskRowExists(slugID) {
		t.Errorf("the refused submit still left row %q in the ledger — the refusal must happen "+
			"before the INSERT", slugID)
	}
	if dir := taskSessionPathForTest(t, slugID); fileExists(dir) {
		t.Errorf("the refused submit still created the session dir %s", dir)
	}
	// The SAME call with a full uuid id still queues: the check is scoped to the
	// condition the dispatch gate uses, not to "any explicit id".
	const uuidID = "01a11500-0000-7000-8000-000000000042"
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "submit fixture uuid", Payload: "work", Repo: repo, TaskID: uuidID,
	})
	if err != nil {
		t.Fatalf("TaskSubmit(--task-id %s) must still queue: %v", uuidID, err)
	}
	if !strings.Contains(out, uuidID) {
		t.Errorf("the confirmation must name the queued row %q:\n%s", uuidID, out)
	}
	if !TaskRowExists(uuidID) {
		t.Errorf("row %q was not written although the submit reported success", uuidID)
	}
}

// taskSessionPathForTest resolves the per-task session container the way the
// submit path does — through the SAME resolver (TaskSessionDir), never a
// hand-rolled path (Rule 13/14).
func taskSessionPathForTest(t *testing.T, taskID string) string {
	t.Helper()
	dir, err := TaskSessionDir(taskID)
	if err != nil {
		t.Fatalf("TaskSessionDir(%s): %v", taskID, err)
	}
	return dir
}
