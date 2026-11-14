package tools

// task_rework_successor_test.go — the archive gate's SUCCESSOR exception, and
// the line it must not cross.
//
// THE LOOP THIS PINS (measured 2026-10-18, merge run 01a14b08): `gopi task
// archive <branch>` refused the branch because the merge run's own §3.6.1 case ④
// rework row — filed one step earlier, precisely so the ref could go — was LIVE
// and named the branch as its deliverable. Ten of eleven refs were removed that
// run; the eleventh stayed, so the next pass filed a DUPLICATE rework row for
// the same branch.
//
// Three arms, so the fix cannot be "proceed whenever a row looks rework-ish":
//
//	① live rework SUCCESSOR  → PROCEED, and the decision names the successor
//	② live PLAIN row         → still REFUSE (the safety line is unchanged)
//	③ decided row            → still REFUSE without --override-completed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// reworkSuccessorLedger points the process at a fresh temp ledger + settings and
// returns the context a seed call takes (the scaffold of Rule 32 — never a
// hand-rolled database path).
func reworkSuccessorLedger(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	return t.Context()
}

// seedReworkSuccessorRow files one ledger row with the fields the successor
// predicate reads: status, deliverable.branch, tags and summary.
func seedReworkSuccessorRow(t *testing.T, ctx context.Context, taskID, status, deliverableBranch, tags, summary string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary, Payload: "p", TaskID: taskID, Tags: tags, Source: "merger",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	deliverable := ""
	if deliverableBranch != "" {
		deliverable = `{"repo":"/repo","branch":"` + deliverableBranch + `"}`
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, deliverable = ?, tags = ?, summary = ? WHERE task_id = ?`,
		status, deliverable, tags, summary, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// TestArchiveGateProceedsOnALiveReworkSuccessor is the RED-PROOF arm: this exact
// ledger shape was REFUSED before the fix (the row is live and the ledger does
// name the branch), and must PROCEED after it.
//
// The shape is the one the merge run actually minted — tags `rework,merge`, a
// deliverable naming the FRESH branch the reworker will create, and the spent
// branch in the summary (`rework <branch>: cannot auto-merge into master`).
func TestArchiveGateProceedsOnALiveReworkSuccessor(t *testing.T) {
	ctx := reworkSuccessorLedger(t)
	const spent = "task/01a1473687cb7823aff395fb56c3ad7c"

	// THE MEASURED SHAPE, reproduced field for field from `gopi task show
	// 01a14b74-3d6a-7994-b329-5d22b95cd88c`: the hand-over lane STAMPS the
	// spent branch into the deliverable (resolveRowStampDeliverableBranch) so
	// its resolver can find the subject, and states the branch as the thing
	// being reworked in the summary. That row is why the archive was refused.
	seedReworkSuccessorRow(t, ctx, "t-successor", TaskStatusPending,
		spent, "rework,merge,conflict",
		"rework "+spent+": cannot auto-merge into master")

	dec, err := ArchiveDecide(spent, ArchiveReasonSuperseded, false)
	if err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	}
	if dec.Verdict != ArchiveVerdictProceed {
		t.Fatalf("a live REWORK SUCCESSOR must not block the archive it succeeds: verdict = %q refusal = %s",
			dec.Verdict, dec.Refusal)
	}
	if dec.SuccessorTaskID != "t-successor" {
		t.Fatalf("the decision must NAME the successor it passed, got %q", dec.SuccessorTaskID)
	}
	if dec.Refusal != "" {
		t.Fatalf("a proceeding decision carries no refusal, got %q", dec.Refusal)
	}
	// The reason the caller gave is kept: this is an ordinary archive.
	if dec.EffectiveReason != ArchiveReasonSuperseded {
		t.Fatalf("effective reason = %q, want %q", dec.EffectiveReason, ArchiveReasonSuperseded)
	}
	// The row is rendered with the exception taken, never silently.
	if got := formatArchiveSuccessorForTest(dec); !strings.Contains(got, "t-successor") {
		t.Fatalf("the successor must be visible in the decision, got %q", got)
	}
}

// TestArchiveGateStillRefusesALivePlainRow is the SAFETY arm: a live row that
// merely NAMES the branch — any status, any deliverable, but a SUBJECT that says
// the branch is what is being BUILT rather than what is being reworked — must
// keep refusing. The exception is one shape, not a general loosening.
func TestArchiveGateStillRefusesALivePlainRow(t *testing.T) {
	ctx := reworkSuccessorLedger(t)
	const branch = "task/01a1473687cb7823aff395fb56c3ad7c"

	// The deliverable IS the branch and the summary says the work happens HERE.
	seedReworkSuccessorRow(t, ctx, "t-tenant", TaskStatusInProgress,
		branch, "feat,impl", "implement "+branch+" locally")
	if dec, err := ArchiveDecide(branch, ArchiveReasonSuperseded, true); err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	} else if dec.Verdict != ArchiveVerdictRefused {
		t.Fatalf("a live row whose subject is work ON the branch must still refuse: verdict = %q", dec.Verdict)
	}

	// And the same subject WITH the rework tag is still not a successor: the tag
	// alone never answers the question.
	seedReworkSuccessorRow(t, ctx, "t-tenant-tagged", TaskStatusInProgress,
		branch, "rework,merge", "rework the "+branch+" implementation in place")
	if dec, err := ArchiveDecide(branch, ArchiveReasonSuperseded, true); err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	} else if dec.Verdict != ArchiveVerdictRefused {
		t.Fatalf("a rework-tagged row whose subject is not a rework OF the branch must refuse: verdict = %q", dec.Verdict)
	}
}

// TestArchiveGateRefusesALiveRowOutsideTheReworkLane — no rework vocabulary in
// the tags: a branch named only in a summary is not a successor claim.
func TestArchiveGateRefusesALiveRowOutsideTheReworkLane(t *testing.T) {
	ctx := reworkSuccessorLedger(t)
	const branch = "task/01a1473687cb7823aff395fb56c3ad7c"
	seedReworkSuccessorRow(t, ctx, "t-plain", TaskStatusInProgress,
		branch, "feat", "rework "+branch+": cannot auto-merge into master")

	if dec, err := ArchiveDecide(branch, ArchiveReasonSuperseded, true); err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	} else if dec.Verdict != ArchiveVerdictRefused {
		t.Fatalf("a live non-rework row must refuse: verdict = %q", dec.Verdict)
	}
}

// TestArchiveGateDecidedRowStillNeedsTheOverride — arm ③: the successor
// exception is about the LIVE case only and must not weaken the completed case,
// whose refusal (and its --override-completed escape) is the epic's core rule.
func TestArchiveGateDecidedRowStillNeedsTheOverride(t *testing.T) {
	ctx := reworkSuccessorLedger(t)
	const branch = "task/01a1473687cb7823aff395fb56c3ad7c"
	seedReworkSuccessorRow(t, ctx, "t-done", TaskStatusCompleted,
		branch, "rework,merge", "rework "+branch+": cannot auto-merge into master")

	dec, err := ArchiveDecide(branch, ArchiveReasonSuperseded, false)
	if err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	}
	if dec.Verdict != ArchiveVerdictRefused {
		t.Fatalf("a COMPLETED owner must still refuse without the override: verdict = %q", dec.Verdict)
	}
	if !strings.Contains(dec.Refusal, "override-completed") {
		t.Fatalf("the refusal must name the override flag: %s", dec.Refusal)
	}
	dec, err = ArchiveDecide(branch, ArchiveReasonSuperseded, true)
	if err != nil {
		t.Fatalf("ArchiveDecide (override): %v", err)
	}
	if dec.Verdict != ArchiveVerdictProceed {
		t.Fatalf("the override must still clear a completed owner: verdict = %q", dec.Verdict)
	}
	if dec.SuccessorTaskID != "" {
		t.Fatalf("the override path is not the successor path; got successor %q", dec.SuccessorTaskID)
	}
}

// TestArchiveLiveRefusalNamesTheOperableEscape — the general LIVE refusal must
// tell the operator WHICH command concludes the row (Rule: a refusal an
// operator cannot act on is a wall, not a gate).
func TestArchiveLiveRefusalNamesTheOperableEscape(t *testing.T) {
	ctx := reworkSuccessorLedger(t)
	const branch = "feat/in-flight"
	seedReworkSuccessorRow(t, ctx, "t-blocking", TaskStatusInProgress, branch, "feat", "build "+branch)

	dec, err := ArchiveDecide(branch, ArchiveReasonHandMistake, true)
	if err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	}
	if dec.Verdict != ArchiveVerdictRefused {
		t.Fatalf("verdict = %q, want refused", dec.Verdict)
	}
	for _, want := range []string{"IN FLIGHT", "gopi task update t-blocking --status " + TaskStatusCancelled, reworkLaneName} {
		if !strings.Contains(dec.Refusal, want) {
			t.Errorf("the live refusal does not name %q:\n%s", want, dec.Refusal)
		}
	}
}

// TestTaskReworkSuccessorOfReadsTheRowItself — the predicate's own unit arms:
// every clause is load-bearing, so a later edit cannot quietly drop one.
func TestTaskReworkSuccessorOfReadsTheRowItself(t *testing.T) {
	const branch = "task/01a1473687cb7823aff395fb56c3ad7c"
	base := reworkRowShape{
		Status: TaskStatusPending, DeliverableBranch: "task/fresh",
		Tags: "rework,merge", Summary: "rework " + branch + ": cannot auto-merge into master",
	}
	if !taskReworkSuccessorOf(base, branch) {
		t.Fatal("the measured shape must qualify as a successor")
	}
	// The hand-over lane STAMPS the spent branch into the deliverable
	// (resolveRowStampDeliverableBranch) so the resolver can find its subject.
	// That is not a disqualifier: the SUMMARY is what says "rework OF".
	stamped := base
	stamped.DeliverableBranch = branch
	if !taskReworkSuccessorOf(stamped, branch) {
		t.Fatal("a hand-over row stamped with the spent branch must still qualify: the deliverable is not the question")
	}
	for _, tc := range []struct {
		name string
		mut  func(r *reworkRowShape)
	}{
		{"not live", func(r *reworkRowShape) { r.Status = TaskStatusCompleted }},
		{"outside the rework lane", func(r *reworkRowShape) { r.Tags = "feat,docs" }},
		{"summary names another branch", func(r *reworkRowShape) { r.Summary = "rework task/other: cannot auto-merge" }},
		{"summary names no branch", func(r *reworkRowShape) { r.Summary = "rework it soon" }},
		{"subject is work ON the branch", func(r *reworkRowShape) {
			r.Summary = "rework the " + branch + " implementation in place"
		}},
	} {
		row := base
		tc.mut(&row)
		if taskReworkSuccessorOf(row, branch) {
			t.Errorf("%s: must NOT qualify as a successor of %s", tc.name, branch)
		}
	}
}

// TestReworkSelectionBranchReadsTheLiveSummaryShapes pins the two spellings the
// lane's writers actually produce: §3.6's hand-over row and the in-code lanes'
// prefixes (a trailing colon or a "(DELETED)" suffix must not hide the branch).
func TestReworkSelectionBranchReadsTheLiveSummaryShapes(t *testing.T) {
	for _, tc := range []struct{ summary, want string }{
		{"rework task/01a14736: cannot auto-merge into master", "task/01a14736"},
		{"resolve merge conflict: rework: task/01a14736", "task/01a14736"},
		{"merge verify rework: task/01a14736", "task/01a14736"},
		{"rework task/01a14736 (DELETED) — start fresh", "task/01a14736"},
		{"rework it soon", ""},
		{"rework the task/01a14736 implementation in place", ""},
		{"", ""},
	} {
		got, ok := reworkSelectionBranch(tc.summary)
		if tc.want == "" {
			if ok {
				t.Errorf("%q: must name no branch, got %q", tc.summary, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("%q: got (%q, %v), want %q", tc.summary, got, ok, tc.want)
		}
	}
}

// formatArchiveSuccessorForTest renders the successor field the way the CLI
// does, from the decision alone (the CLI renderer takes the ArchiveBranchResult;
// this keeps the assertion at the tools layer where the decision lives).
func formatArchiveSuccessorForTest(dec ArchiveDecision) string {
	if dec.SuccessorTaskID == "" {
		return ""
	}
	return "successor: task " + dec.SuccessorTaskID + " (" + ArchiveReworkLaneName() + ")"
}
