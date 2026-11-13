package tools

// RED PROOF for the deliverable-tip criterion (epic 01a13bbb). This file is
// written against the PRE-FIX API ONLY — every symbol it calls (TaskSubmit /
// seedDeliverable / deadHolderReclaimFixture / gitRun / covLifeGit /
// TaskStart / TaskRunWriteback / openTestTasksDB / memoryDBPath /
// removalaudit.BranchDeleteAt / removalaudit.EnvSink) exists on master — so the
// SAME file compiles on both trees and the two runs differ by the fix alone:
//
//	# baseline (a master worktree) — copy THIS FILE ONLY
//	git worktree add --detach <scratch>/tip-red master
//	cp internal/tools/zz_redproof_deliverable_tip_test.go <scratch>/tip-red/internal/tools/
//	cd <scratch>/tip-red && \
//	  go test -count=1 -v -run 'TestZZRedProof_DeliverableTipUnreachable' ./internal/tools/
//	--- FAIL: ... the row claims `completed` while its own deliverable branch
//	            resolves to no ref and NOTHING on its trail says so
//
//	# this branch
//	go test -count=1 -run 'TestZZRedProof_DeliverableTipUnreachable' ./internal/tools/
//	--- PASS
//
// The property: a row written back `completed` whose recorded deliverable branch
// resolves to no ref (its object alive, no ref holding it — the HOST fact
// measured on 01a13ae1) must leave an ATTRIBUTED record on its own trail naming
// the tip at risk. Pre-fix nothing is recorded at all, so the loss is silent:
// every surface renders that row exactly like a delivered one.
import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/removalaudit"
)

// zzRedProofTipMarker is the sentence's load-bearing phrase, spelled as a
// LITERAL here on purpose: the constant does not exist on the pre-fix tree, and
// this file must compile on both.
const zzRedProofTipMarker = "its deliverable's tip is reachable from no ref"

func TestZZRedProof_DeliverableTipUnreachableIsRecorded(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), "removal-audit.log"))
	const taskID = "t-red-tip"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, branch, wt)
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
	// The incident's own HOST state: the object is alive, and no ref holds it.
	tip := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "refs/heads/"+branch))
	if tip == "" {
		t.Fatalf("fixture: refs/heads/%s did not resolve", branch)
	}
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranchForce, repo, branch, taskID, tip,
		"", "red proof fixture: the close-out chain's own force delete")
	gitRun(t, repo, "worktree", "remove", wt)
	gitRun(t, repo, "update-ref", "-d", "refs/heads/"+branch)
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "cat-file", "-t", tip)); got != "commit" {
		t.Fatalf("fixture: the tip must still exist as an object, got %q", got)
	}
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "for-each-ref", "--contains", tip, "--format=%(refname)")); got != "" {
		t.Fatalf("fixture: no ref may contain the tip, got %q", got)
	}
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND detail LIKE ?`,
		taskID, "%"+zzRedProofTipMarker+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatalf("SILENT LOSS: the row claims `%s` while its own deliverable %s resolves to no ref "+
			"(tip %s still a live object, `for-each-ref --contains` EMPTY) and NOTHING on the row's "+
			"trail says so — every surface renders it exactly like a delivered row",
			TaskStatusCompleted, branch, tip)
	}
	var detail string
	if err := db.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND detail LIKE ? ORDER BY id DESC LIMIT 1`,
		taskID, "%"+zzRedProofTipMarker+"%").Scan(&detail); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{taskID, branch, tip} {
		if !strings.Contains(detail, want) {
			t.Errorf("the record must name %q — got %q", want, detail)
		}
	}
}

// The CONTROL arm, in the red-proof file so both halves travel together: a row
// whose deliverable records no branch is untouched by the criterion (no record
// filed, writeback unaffected) — on the pre-fix tree this PASSES too, which is
// exactly what a control group is for.
func TestZZRedProof_DeliverableTipControlCaseIsUntouched(t *testing.T) {
	repo, _, _ := deadHolderReclaimFixture(t)
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), "removal-audit.log"))
	const taskID = "t-red-tip-none"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, "", "")
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("writeback of a row that names no branch must be untouched: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND detail LIKE ?`,
		taskID, "%"+zzRedProofTipMarker+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a row that records NO branch must never be judged — got %d record(s)", n)
	}
}
