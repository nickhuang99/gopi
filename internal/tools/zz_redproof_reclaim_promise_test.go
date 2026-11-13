package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestZZRedProofFalseReclaimPromise is the A/B red proof for the 2026-09-13
// defect and it is deliberately written against the PRE-FIX API ONLY
// (MergerSweepPreview/DescribeProtected — no field added by the fix), so the
// very same file compiles and runs on both trees:
//
//	pre-fix tree (15ffde2f): FAILS — the operator line says "reclaimable by the
//	  finish-cleanup sweep" for a DIRTY worktree, i.e. for one the reclaim arm's
//	  guard ② refuses by design.
//	post-fix tree: PASSES — the line says the worktree still blocks the merge
//	  and names the reason.
//
// Reproduce:
//
//	git worktree add --detach /tmp/gopi-redproof 15ffde2f
//	cp internal/tools/zz_redproof_reclaim_promise_test.go /tmp/gopi-redproof/internal/tools/
//	cd /tmp/gopi-redproof && go test -run TestZZRedProofFalseReclaimPromise ./internal/tools/
func TestZZRedProofFalseReclaimPromise(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-redproof", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-redproof", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))
	// The measured shape: the dead holder left uncommitted work behind.
	if err := os.WriteFile(filepath.Join(wt, "uncommitted.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}

	preview := MergerSweepPreview(repo)
	if len(preview.DeadHolder) != 1 || preview.DeadHolder[0] != branch {
		t.Fatalf("precondition: the dead holder's branch must be named, got %v", preview.DeadHolder)
	}
	line := preview.DescribeProtected()
	if strings.Contains(line, "reclaimable by the finish-cleanup sweep") {
		t.Fatalf("FALSE PROMISE: a DIRTY dead-holder worktree is refused by the reclaim arm's guard ②, yet the operator line promises a release: %q", line)
	}
	if !strings.Contains(line, branch) {
		t.Errorf("the operator line must name the withheld branch, got %q", line)
	}
	if !strings.Contains(line, "DIRTY") {
		t.Errorf("the operator line must name WHY the branch is still refused (dirty), got %q", line)
	}
}
